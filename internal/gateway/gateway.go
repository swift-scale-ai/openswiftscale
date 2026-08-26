package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/auth"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/config"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/limit"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/provider"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/router"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/store"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/webui"
)

type Gateway struct {
	cfg       config.Config
	auth      *auth.Authenticator
	router    *router.Router
	providers *provider.Client
	store     *store.Store
	logger    *slog.Logger
	limiter   *limit.Limiter
	requests  atomic.Uint64
	errors    atomic.Uint64
	startedAt time.Time
}

func New(cfg config.Config, c *catalog.Catalog, a *auth.Authenticator, r *router.Router, p *provider.Client, s *store.Store, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{cfg: cfg, auth: a, router: r, providers: p, store: s, logger: logger, limiter: limit.New(cfg.RateLimitRPM, cfg.Concurrency), startedAt: time.Now()}
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", webui.Handler())
	mux.Handle("GET /assets/", webui.Handler())
	mux.HandleFunc("GET /healthz", g.health)
	mux.HandleFunc("GET /readyz", g.ready)
	mux.HandleFunc("GET /metrics", g.metrics)
	mux.HandleFunc("GET /v1/models", g.withAPIAuth(g.listModels))
	mux.HandleFunc("POST /v1/chat/completions", g.withAPIAuth(g.inference("chat")))
	mux.HandleFunc("POST /v1/responses", g.withAPIAuth(g.inference("responses")))
	mux.HandleFunc("POST /v1/embeddings", g.withAPIAuth(g.inference("embeddings")))
	mux.HandleFunc("GET /api/admin/status", g.withManagementAuth(g.adminStatus))
	mux.HandleFunc("GET /api/admin/models", g.withManagementAuth(g.adminModels))
	mux.HandleFunc("GET /api/admin/model-route-policies", g.withManagementAuth(g.adminModelRoutePolicies))
	mux.HandleFunc("POST /api/admin/model-route-policies", g.withManagementAuth(g.saveModelRoutePolicy))
	mux.HandleFunc("DELETE /api/admin/model-route-policies/{id}", g.withManagementAuth(g.deleteModelRoutePolicy))
	mux.HandleFunc("GET /api/admin/routing-rules", g.withManagementAuth(g.adminRoutingRules))
	mux.HandleFunc("POST /api/admin/routing-rules", g.withManagementAuth(g.saveRoutingRule))
	mux.HandleFunc("DELETE /api/admin/routing-rules/{id}", g.withManagementAuth(g.deleteRoutingRule))
	mux.HandleFunc("GET /api/admin/providers", g.withManagementAuth(g.adminProviders))
	mux.HandleFunc("POST /api/admin/providers", g.withManagementAuth(g.saveProvider))
	mux.HandleFunc("DELETE /api/admin/providers/{id}", g.withManagementAuth(g.deleteProvider))
	mux.HandleFunc("GET /api/admin/usage", g.withManagementAuth(g.adminUsage))
	mux.HandleFunc("GET /api/admin/users", g.withManagementAuth(g.adminAPIUsers))
	mux.HandleFunc("POST /api/admin/users", g.withManagementAuth(g.createAPIUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", g.withManagementAuth(g.updateAPIUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", g.withManagementAuth(g.deleteAPIUser))
	mux.HandleFunc("POST /api/admin/users/{id}/keys", g.withManagementAuth(g.createAPIKey))
	mux.HandleFunc("DELETE /api/admin/users/{id}/keys/{key_id}", g.withManagementAuth(g.deleteAPIKey))
	return g.middleware(mux)
}

func (g *Gateway) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-OpenSwiftScale-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (g *Gateway) withAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyID := auth.KeyID(r)
		valid := g.auth.Authenticate(r)
		if !valid {
			var err error
			keyID, valid, err = g.store.AuthenticateAPIKey(r.Context(), auth.BearerToken(r))
			if err != nil {
				g.logger.Error("authenticate managed API key", "error", err)
				writeError(w, http.StatusInternalServerError, "authentication_unavailable", "API key authentication is temporarily unavailable.")
				return
			}
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "A valid OpenSwiftScale API key is required.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), apiKeyIDKey{}, keyID)))
	}
}

func (g *Gateway) withManagementAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		validBasic := ok && secureEqual(username, g.cfg.AdminUsername) && secureEqual(password, g.cfg.AdminPassword)

		// Bearer support is intentionally temporary so existing API clients can
		// migrate without downtime. The web console always uses Basic auth.
		legacyToken := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		validLegacy := legacyToken != "" && secureEqual(legacyToken, g.cfg.AdminPassword)
		if !validBasic && !validLegacy {
			w.Header().Set("WWW-Authenticate", `Basic realm="OpenSwiftScale Console", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "invalid_admin_credentials", "The administrator username or password is incorrect.")
			return
		}
		next(w, r)
	}
}

func secureEqual(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func (g *Gateway) listModels(w http.ResponseWriter, r *http.Request) {
	runtimeCatalog := g.router.Catalog()
	type publicModel struct {
		contextWindow int
		capabilities  []string
		seen          map[string]bool
	}
	models := make(map[string]*publicModel)
	order := make([]string, 0)
	for _, model := range runtimeCatalog.SortedModels() {
		if !g.router.Available(model) {
			continue
		}
		item := models[model.ID]
		if item == nil {
			item = &publicModel{seen: make(map[string]bool)}
			models[model.ID] = item
			order = append(order, model.ID)
		}
		if model.ContextWindow > item.contextWindow {
			item.contextWindow = model.ContextWindow
		}
		for _, capability := range model.Capabilities {
			if !item.seen[capability] {
				item.capabilities = append(item.capabilities, capability)
				item.seen[capability] = true
			}
		}
	}
	data := make([]map[string]any, 0, len(order))
	for _, modelID := range order {
		model := models[modelID]
		data = append(data, map[string]any{
			"id": modelID, "object": "model", "owned_by": "openswiftscale",
			"context_window": model.contextWindow, "capabilities": model.capabilities,
		})
	}
	rules, err := g.store.RoutingRules(r.Context())
	if err == nil {
		for _, rule := range rules {
			if !rule.Enabled || !g.router.RuleAvailable(rule.ID) {
				continue
			}
			capabilities, seenCapabilities := []string{}, map[string]bool{}
			contextWindow := 0
			for _, member := range rule.Members {
				for _, model := range runtimeCatalog.ModelsByID(member.ModelID) {
					if model.ContextWindow > contextWindow {
						contextWindow = model.ContextWindow
					}
					for _, capability := range model.Capabilities {
						if !seenCapabilities[capability] {
							capabilities = append(capabilities, capability)
							seenCapabilities[capability] = true
						}
					}
				}
			}
			data = append(data, map[string]any{"id": rule.ID, "object": "model", "owned_by": "openswiftscale-routing-rule",
				"context_window": contextWindow, "capabilities": capabilities, "routing_rule": true})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (g *Gateway) inference(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		g.requests.Add(1)
		release, err := g.limiter.Acquire(apiKeyIDFrom(r))
		if err != nil {
			g.errors.Add(1)
			writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded", err.Error())
			return
		}
		defer release()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, g.cfg.MaxBodyBytes))
		if err != nil {
			g.errors.Add(1)
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds the configured limit.")
			return
		}
		var envelope struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || strings.TrimSpace(envelope.Model) == "" {
			g.errors.Add(1)
			writeError(w, http.StatusBadRequest, "invalid_request", "A JSON body with a model field is required.")
			return
		}
		routes, err := g.router.Resolve(envelope.Model, endpoint)
		if err != nil {
			g.errors.Add(1)
			status, code := http.StatusBadRequest, "model_not_found"
			if errors.Is(err, router.ErrModelUnavailable) {
				status, code = http.StatusServiceUnavailable, "model_unavailable"
			} else if errors.Is(err, router.ErrCapabilityMismatch) {
				code = "unsupported_model_capability"
			}
			writeError(w, status, code, err.Error())
			return
		}

		requestID := requestIDFrom(r.Context())
		var selected router.Route
		var response *http.Response
		for index, route := range routes {
			selected = route
			response, err = g.providers.Do(r.Context(), endpoint, route, body, r.Header)
			g.recordProviderValidation(route.Provider.ID, response, err)
			if err == nil && !(provider.RetryableStatus(response.StatusCode) && index < len(routes)-1) {
				break
			}
			if response != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				_ = response.Body.Close()
			}
		}
		if err != nil || response == nil {
			g.errors.Add(1)
			g.record(r, selected, endpoint, http.StatusBadGateway, provider.Usage{}, started)
			writeError(w, http.StatusBadGateway, "provider_unavailable", "The configured provider could not be reached.")
			return
		}
		w.Header().Set("X-OpenSwiftScale-Provider", selected.Provider.ID)
		w.Header().Set("X-OpenSwiftScale-Route-Priority", strconv.Itoa(selected.Model.Priority))
		if selected.RuleID != "" {
			w.Header().Set("X-OpenSwiftScale-Routing-Rule", selected.RuleID)
			w.Header().Set("X-OpenSwiftScale-Resolved-Model", selected.Model.ID)
			w.Header().Set("X-OpenSwiftScale-Model-Priority", strconv.Itoa(selected.RulePriority))
		}
		usage, copyErr := g.providers.CopyResponse(w, response, selected, requestID)
		g.record(r, selected, endpoint, response.StatusCode, usage, started)
		if response.StatusCode >= 400 || copyErr != nil {
			g.errors.Add(1)
		}
		if copyErr != nil {
			g.logger.Warn("provider response interrupted", "request_id", requestID, "provider", selected.Provider.ID, "error", copyErr)
		}
	}
}

func (g *Gateway) recordProviderValidation(providerID string, response *http.Response, callErr error) {
	status, message := "", ""
	switch {
	case callErr != nil:
		status, message = "temporarily_unavailable", "Provider could not be reached."
	case response == nil:
		return
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		status, message = "invalid", "Provider rejected the configured credential."
	case response.StatusCode < http.StatusBadRequest:
		status = "valid"
	case response.StatusCode >= http.StatusInternalServerError:
		status, message = "temporarily_unavailable", "Provider returned a server error."
	default:
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.store.SetProviderValidation(ctx, providerID, status, message); err != nil {
		g.logger.Warn("update provider validation", "provider", providerID, "error", err)
	}
}

func (g *Gateway) record(r *http.Request, route router.Route, endpoint string, status int, usage provider.Usage, started time.Time) {
	modelID, providerID := route.Model.ID, route.Provider.ID
	cost := float64(usage.PromptTokens)/1_000_000*route.Model.Pricing.InputPerMillion + float64(usage.CompletionTokens)/1_000_000*route.Model.Pricing.OutputPerMillion
	event := store.Usage{
		RequestID: requestIDFrom(r.Context()), KeyID: apiKeyIDFrom(r), Model: modelID, Provider: providerID,
		Endpoint: endpoint, Status: status, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		CostUSD: cost, LatencyMS: time.Since(started).Milliseconds(), CreatedAt: time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := g.store.Record(ctx, event); err != nil {
		g.logger.Error("record usage", "request_id", event.RequestID, "error", err)
	}
}

func (g *Gateway) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (g *Gateway) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := g.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "database": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "database": "ok"})
}

func (g *Gateway) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# TYPE openswiftscale_requests_total counter\nopenswiftscale_requests_total %d\n", g.requests.Load())
	_, _ = fmt.Fprintf(w, "# TYPE openswiftscale_errors_total counter\nopenswiftscale_errors_total %d\n", g.errors.Load())
}

func (g *Gateway) adminStatus(w http.ResponseWriter, r *http.Request) {
	runtimeCatalog := g.router.Catalog()
	modelIDs := make(map[string]bool)
	availableIDs := make(map[string]bool)
	for _, model := range runtimeCatalog.Models {
		modelIDs[model.ID] = true
		if g.router.ModelAvailable(model.ID) {
			availableIDs[model.ID] = true
		}
	}
	rules, _ := g.store.RoutingRules(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "OpenSwiftScale", "version": Version, "uptime_seconds": int64(time.Since(g.startedAt).Seconds()),
		"models": len(modelIDs), "available_models": len(availableIDs), "routes": len(runtimeCatalog.Models), "routing_rules": len(rules), "providers": len(runtimeCatalog.Providers), "telemetry": false, "prompt_logging": g.cfg.LogPrompts,
		"api_base_url": g.apiBaseURL(r),
	})
}

func (g *Gateway) apiBaseURL(r *http.Request) string {
	origin := strings.TrimRight(g.cfg.PublicURL, "/")
	if origin == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	return origin + "/v1"
}

func (g *Gateway) adminModels(w http.ResponseWriter, _ *http.Request) {
	models := g.router.Catalog().SortedModels()
	type item struct {
		catalog.Model
		Available bool `json:"available"`
	}
	out := make([]item, 0, len(models))
	for _, model := range models {
		out = append(out, item{Model: model, Available: g.router.Available(model)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) adminModelRoutePolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := g.store.ModelRoutePolicies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policies)
}

type modelRoutePolicyInput struct {
	ModelID  string `json:"model_id"`
	Strategy string `json:"strategy"`
}

func (g *Gateway) saveModelRoutePolicy(w http.ResponseWriter, r *http.Request) {
	var input modelRoutePolicyInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid model route policy is required.")
		return
	}
	input.ModelID = strings.ToLower(strings.TrimSpace(input.ModelID))
	if _, exists := g.router.Catalog().ModelByID(input.ModelID); !exists {
		writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("Model %q does not exist.", input.ModelID))
		return
	}
	switch input.Strategy {
	case "failover", "load-balance", "hybrid":
	default:
		writeError(w, http.StatusBadRequest, "invalid_strategy", "Strategy must be failover, load-balance, or hybrid.")
		return
	}
	policy := store.ModelRoutePolicy{ModelID: input.ModelID, Strategy: input.Strategy}
	if err := g.store.SaveModelRoutePolicy(r.Context(), policy); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (g *Gateway) deleteModelRoutePolicy(w http.ResponseWriter, r *http.Request) {
	if err := g.store.DeleteModelRoutePolicy(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) adminRoutingRules(w http.ResponseWriter, r *http.Request) {
	rules, err := g.store.RoutingRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

type routingRuleInput struct {
	ID      string                      `json:"id"`
	Name    string                      `json:"name"`
	Enabled *bool                       `json:"enabled"`
	Members []catalog.RoutingRuleMember `json:"members"`
}

func (g *Gateway) saveRoutingRule(w http.ResponseWriter, r *http.Request) {
	var input routingRuleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid routing rule is required.")
		return
	}
	input.ID = strings.ToLower(strings.TrimSpace(input.ID))
	if !connectionIDPattern.MatchString(input.ID) {
		writeError(w, http.StatusBadRequest, "invalid_rule_id", "Route ID must contain only lowercase letters, numbers, dots, dashes, or underscores.")
		return
	}
	if _, exists := g.router.Catalog().ModelByID(input.ID); exists {
		writeError(w, http.StatusConflict, "rule_id_conflict", "Route ID must not duplicate a Public Model ID.")
		return
	}
	if len(input.Members) == 0 {
		writeError(w, http.StatusBadRequest, "members_required", "A routing rule requires at least one model.")
		return
	}
	seen := make(map[string]bool)
	catalogSnapshot := g.router.Catalog()
	for index := range input.Members {
		member := &input.Members[index]
		member.ModelID = strings.ToLower(strings.TrimSpace(member.ModelID))
		if seen[member.ModelID] {
			writeError(w, http.StatusBadRequest, "duplicate_member", "Each model can appear only once in a routing rule.")
			return
		}
		seen[member.ModelID] = true
		if _, exists := catalogSnapshot.ModelByID(member.ModelID); !exists {
			writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("Model %q does not exist.", member.ModelID))
			return
		}
		priority, weight, err := normalizeRoutePolicy(member.Priority, member.Weight)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_rule_member", err.Error())
			return
		}
		member.Priority, member.Weight = priority, weight
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = input.ID
	}
	rule := catalog.RoutingRule{ID: input.ID, Name: name, Enabled: enabled, Members: input.Members}
	if err := g.store.SaveRoutingRule(r.Context(), rule); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	if err := g.refreshRuntime(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "runtime_refresh_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (g *Gateway) deleteRoutingRule(w http.ResponseWriter, r *http.Request) {
	if err := g.store.DeleteRoutingRule(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
		return
	}
	if err := g.refreshRuntime(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "runtime_refresh_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) adminProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := g.store.Providers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	type item struct {
		store.ProviderConnection
		Models []catalog.Model `json:"models"`
	}
	runtimeCatalog := g.router.Catalog()
	out := make([]item, 0, len(providers))
	for _, connection := range providers {
		entry := item{ProviderConnection: connection}
		for _, model := range runtimeCatalog.Models {
			if model.Provider == connection.ID {
				entry.Models = append(entry.Models, model)
			}
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

type providerInput struct {
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Type              string             `json:"type"`
	BaseURL           string             `json:"base_url"`
	Authentication    string             `json:"authentication"`
	APIKeyHeader      string             `json:"api_key_header"`
	APIKey            *string            `json:"api_key"`
	ClearAPIKey       bool               `json:"clear_api_key"`
	Enabled           *bool              `json:"enabled"`
	AllowInsecureHTTP bool               `json:"allow_insecure_http"`
	ModelCategory     string             `json:"model_category"`
	Model             *modelInput        `json:"model,omitempty"`
	Routes            []routePolicyInput `json:"routes,omitempty"`
}

type routePolicyInput struct {
	ModelID  string `json:"model_id"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
}

type modelInput struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Family          string   `json:"family"`
	UpstreamModel   string   `json:"upstream_model"`
	Capabilities    []string `json:"capabilities"`
	ContextWindow   int      `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	Priority        int      `json:"priority"`
	Weight          int      `json:"weight"`
}

var connectionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

var defaultModelCapabilities = []string{"chat", "responses", "streaming", "tools", "reasoning", "structured_output", "vision", "embeddings"}

func (g *Gateway) saveProvider(w http.ResponseWriter, r *http.Request) {
	var input providerInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid provider configuration is required.")
		return
	}
	input.ID = strings.ToLower(strings.TrimSpace(input.ID))
	if !connectionIDPattern.MatchString(input.ID) {
		writeError(w, http.StatusBadRequest, "invalid_provider_id", "Provider ID must contain only lowercase letters, numbers, dots, dashes, or underscores.")
		return
	}
	existing, found, err := g.store.Provider(r.Context(), input.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	} else if found {
		enabled = existing.Enabled
	}
	p := catalog.Provider{ID: input.ID, Type: strings.ToLower(strings.TrimSpace(input.Type)), BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Authentication: strings.ToLower(strings.TrimSpace(input.Authentication)), APIKeyHeader: strings.TrimSpace(input.APIKeyHeader)}
	official := found && existing.Official
	modelCategory := strings.ToLower(strings.TrimSpace(input.ModelCategory))
	if official {
		modelCategory = ""
		p = existing.Provider
		if strings.TrimSpace(input.BaseURL) != "" {
			p.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
		}
	} else {
		if modelCategory == "" && found {
			modelCategory = existing.ModelCategory
		}
		if modelCategory == "" {
			modelCategory = "open-source"
		}
		if modelCategory != "open-source" && modelCategory != "commercial" {
			writeError(w, http.StatusBadRequest, "invalid_model_category", "Model category must be open-source or commercial.")
			return
		}
		if p.Type != "openai-compatible" && p.Type != "anthropic" {
			writeError(w, http.StatusBadRequest, "invalid_protocol", "Protocol must be openai-compatible or anthropic.")
			return
		}
		if err := validateProviderURL(p.BaseURL, input.AllowInsecureHTTP); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_base_url", err.Error())
			return
		}
		p.ChatPath, p.ResponsesPath, p.EmbeddingsPath = customEndpointPaths(p.BaseURL, p.Type)
		if p.Type == "anthropic" {
			if p.Authentication == "" {
				p.Authentication, p.APIKeyHeader = "x-api-key", "x-api-key"
			}
		}
	}
	if err := validateProviderURL(p.BaseURL, input.AllowInsecureHTTP); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_base_url", err.Error())
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" && found {
		name = existing.Name
	}
	apiKey := input.APIKey
	if input.ClearAPIKey {
		empty := ""
		apiKey = &empty
	}
	update := store.ConnectionUpdate{Provider: p, Name: name, Official: official, ModelCategory: modelCategory, Enabled: enabled, APIKey: apiKey}
	for _, route := range input.Routes {
		modelID := strings.ToLower(strings.TrimSpace(route.ModelID))
		if !connectionIDPattern.MatchString(modelID) {
			writeError(w, http.StatusBadRequest, "invalid_route", "A valid Public Model ID is required for every route policy.")
			return
		}
		priority, weight, err := normalizeRoutePolicy(route.Priority, route.Weight)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_route", err.Error())
			return
		}
		update.Routes = append(update.Routes, store.RoutePolicyUpdate{ModelID: modelID, Priority: priority, Weight: weight})
	}
	if !official {
		if input.Model == nil {
			writeError(w, http.StatusBadRequest, "model_required", "A custom connection requires at least one model.")
			return
		}
		model, err := normalizeModelInput(*input.Model, p.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_model", err.Error())
			return
		}
		update.Models = []catalog.Model{model}
	}
	if err := g.store.SaveConnection(r.Context(), update); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	if err := g.refreshRuntime(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "runtime_refresh_failed", err.Error())
		return
	}
	connection, _, _ := g.store.Provider(r.Context(), p.ID)
	writeJSON(w, http.StatusOK, connection)
}

func (g *Gateway) deleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := g.store.DeleteConnection(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
		return
	}
	if err := g.refreshRuntime(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "runtime_refresh_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) refreshRuntime(ctx context.Context) error {
	runtimeCatalog, keys, err := g.store.RuntimeCatalog(ctx)
	if err != nil {
		return err
	}
	rules, err := g.store.RoutingRules(ctx)
	if err != nil {
		return err
	}
	g.router.Replace(runtimeCatalog, keys, rules)
	return nil
}

func validateProviderURL(value string, allowInsecure bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Base URL must be an absolute URL without credentials, query parameters, or fragments.")
	}
	if parsed.Scheme != "https" && !(allowInsecure && parsed.Scheme == "http") {
		return errors.New("HTTPS is required unless insecure HTTP is explicitly enabled for a trusted local endpoint.")
	}
	return nil
}

func customEndpointPaths(baseURL, protocol string) (string, string, string) {
	parsed, _ := url.Parse(baseURL)
	prefix := "/v1"
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		prefix = ""
	}
	if protocol == "anthropic" {
		return prefix + "/messages", "", ""
	}
	return prefix + "/chat/completions", prefix + "/responses", prefix + "/embeddings"
}

func normalizeModelInput(input modelInput, providerID string) (catalog.Model, error) {
	id := strings.ToLower(strings.TrimSpace(input.ID))
	upstream := strings.TrimSpace(input.UpstreamModel)
	if !connectionIDPattern.MatchString(id) || upstream == "" {
		return catalog.Model{}, errors.New("Model ID and upstream model ID are required.")
	}
	capabilities := input.Capabilities
	if len(capabilities) == 0 {
		capabilities = append([]string(nil), defaultModelCapabilities...)
	} else {
		allowed := make(map[string]bool, len(defaultModelCapabilities))
		for _, capability := range defaultModelCapabilities {
			allowed[capability] = true
		}
		seen := make(map[string]bool, len(capabilities))
		normalized := make([]string, 0, len(capabilities))
		for _, capability := range capabilities {
			capability = strings.ToLower(strings.TrimSpace(capability))
			if !allowed[capability] {
				return catalog.Model{}, fmt.Errorf("unsupported capability %q", capability)
			}
			if !seen[capability] {
				normalized = append(normalized, capability)
				seen[capability] = true
			}
		}
		capabilities = normalized
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = upstream
	}
	priority, weight, err := normalizeRoutePolicy(input.Priority, input.Weight)
	if err != nil {
		return catalog.Model{}, err
	}
	return catalog.Model{ID: id, Name: name, Family: strings.ToLower(strings.TrimSpace(input.Family)), Provider: providerID,
		UpstreamModel: upstream, Capabilities: capabilities, ContextWindow: input.ContextWindow,
		MaxOutputTokens: input.MaxOutputTokens, Pricing: catalog.Pricing{Currency: "USD"}, Priority: priority, Weight: weight}, nil
}

func normalizeRoutePolicy(priority, weight int) (int, int, error) {
	if priority == 0 {
		priority = 100
	}
	if priority < 1 || priority > 10_000 {
		return 0, 0, errors.New("Route priority must be between 1 and 10000.")
	}
	if weight == 0 {
		weight = 100
	}
	if weight < 1 || weight > 10_000 {
		return 0, 0, errors.New("Route weight must be between 1 and 10000.")
	}
	return priority, weight, nil
}

func (g *Gateway) adminUsage(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	recent, err := g.store.Recent(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	summary, err := g.store.Summary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary, "recent": recent})
}

type apiUserInput struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Enabled *bool  `json:"enabled"`
}

func (g *Gateway) adminAPIUsers(w http.ResponseWriter, r *http.Request) {
	users, err := g.store.APIUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (g *Gateway) createAPIUser(w http.ResponseWriter, r *http.Request) {
	var input apiUserInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid API user is required.")
		return
	}
	input.Name, input.Email = strings.TrimSpace(input.Name), strings.TrimSpace(input.Email)
	if input.Name == "" || len(input.Name) > 100 || len(input.Email) > 254 {
		writeError(w, http.StatusBadRequest, "invalid_user", "Name is required and must be at most 100 characters.")
		return
	}
	id, err := randomIdentifier("usr_", 9)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generation_failed", "Could not create the API user.")
		return
	}
	now := time.Now().UTC()
	user := store.APIUser{ID: id, Name: input.Name, Email: input.Email, Enabled: true, Keys: []store.APIKey{}, CreatedAt: now, UpdatedAt: now}
	if err := g.store.CreateAPIUser(r.Context(), user); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (g *Gateway) updateAPIUser(w http.ResponseWriter, r *http.Request) {
	var input apiUserInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid API user update is required.")
		return
	}
	input.Name, input.Email = strings.TrimSpace(input.Name), strings.TrimSpace(input.Email)
	if input.Name == "" || len(input.Name) > 100 || len(input.Email) > 254 || input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_user", "Name and enabled state are required.")
		return
	}
	if err := g.store.UpdateAPIUser(r.Context(), r.PathValue("id"), input.Name, input.Email, *input.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) deleteAPIUser(w http.ResponseWriter, r *http.Request) {
	if err := g.store.DeleteAPIUser(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A valid API key name is required.")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_key_name", "API key name is required and must be at most 100 characters.")
		return
	}
	keyID, err := randomIdentifier("key_", 9)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generation_failed", "Could not create the API key.")
		return
	}
	secret, err := randomIdentifier("ossk_", 32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generation_failed", "Could not create the API key.")
		return
	}
	prefix := secret
	if len(prefix) > 13 {
		prefix = prefix[:13]
	}
	key := store.APIKey{ID: keyID, UserID: r.PathValue("id"), Name: input.Name, Prefix: prefix, Enabled: true, CreatedAt: time.Now().UTC()}
	if err := g.store.CreateAPIKey(r.Context(), key, secret); err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": secret, "key": key})
}

func (g *Gateway) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := g.store.DeleteAPIKey(r.Context(), r.PathValue("id"), r.PathValue("key_id")); err != nil {
		writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func randomIdentifier(prefix string, size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message, "type": "openswiftscale_error", "code": code}})
}

type requestIDKey struct{}
type apiKeyIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func apiKeyIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(apiKeyIDKey{}).(string); ok && value != "" {
		return value
	}
	return auth.KeyID(r)
}

func newRequestID() string {
	return fmt.Sprintf("os_%x", time.Now().UnixNano())
}

var Version = "dev"

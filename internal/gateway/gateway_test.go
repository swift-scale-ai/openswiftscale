package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/auth"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/config"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/provider"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/router"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/secret"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/store"
)

func TestManagedAPIUserKeyLifecycle(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "managed-keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	c := &catalog.Catalog{}
	cfg := config.Config{APIKeys: []string{"bootstrap-key"}, AdminUsername: "admin", AdminPassword: "management-key", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
	app := New(cfg, c, auth.New(cfg.APIKeys, false), router.New(c, nil), provider.NewClient(nil), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	userRequest := httptest.NewRequest(http.MethodPost, "/api/admin/users", strings.NewReader(`{"name":"Build service","email":"build@example.com"}`))
	userRequest.SetBasicAuth("admin", "management-key")
	userResponse := httptest.NewRecorder()
	app.ServeHTTP(userResponse, userRequest)
	if userResponse.Code != http.StatusCreated {
		t.Fatalf("create user: status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var user store.APIUser
	if err := json.Unmarshal(userResponse.Body.Bytes(), &user); err != nil || user.ID == "" {
		t.Fatalf("invalid user response: %#v %v", user, err)
	}

	keyRequest := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+user.ID+"/keys", strings.NewReader(`{"name":"CI"}`))
	keyRequest.SetBasicAuth("admin", "management-key")
	keyResponse := httptest.NewRecorder()
	app.ServeHTTP(keyResponse, keyRequest)
	if keyResponse.Code != http.StatusCreated {
		t.Fatalf("create key: status=%d body=%s", keyResponse.Code, keyResponse.Body.String())
	}
	var created struct {
		APIKey string       `json:"api_key"`
		Key    store.APIKey `json:"key"`
	}
	if err := json.Unmarshal(keyResponse.Body.Bytes(), &created); err != nil || !strings.HasPrefix(created.APIKey, "ossk_") {
		t.Fatalf("invalid key response: %#v %v", created, err)
	}

	modelsRequest := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsRequest.Header.Set("Authorization", "Bearer "+created.APIKey)
	modelsResponse := httptest.NewRecorder()
	app.ServeHTTP(modelsResponse, modelsRequest)
	if modelsResponse.Code != http.StatusOK {
		t.Fatalf("managed key was not accepted: status=%d body=%s", modelsResponse.Code, modelsResponse.Body.String())
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+user.ID+"/keys/"+created.Key.ID, nil)
	revokeRequest.SetBasicAuth("admin", "management-key")
	revokeResponse := httptest.NewRecorder()
	app.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("revoke key: status=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}

	deniedRequest := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	deniedRequest.Header.Set("Authorization", "Bearer "+created.APIKey)
	deniedResponse := httptest.NewRecorder()
	app.ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key remained valid: status=%d body=%s", deniedResponse.Code, deniedResponse.Body.String())
	}
}

func TestInferenceAndUsage(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "mock", Type: "openai-compatible", BaseURL: "https://provider.example"}},
		Models:    []catalog.Model{{ID: "public-model", Name: "Public Model", Provider: "mock", UpstreamModel: "provider-model", Capabilities: []string{"chat"}, Pricing: catalog.Pricing{InputPerMillion: 1, OutputPerMillion: 2}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	keys := map[string]string{"mock": "provider-key"}
	client := &http.Client{Transport: gatewayRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)), Request: r}, nil
	})}
	cfg := config.Config{APIKeys: []string{"client-key"}, AdminUsername: "admin", AdminPassword: "management-key", PublicURL: "http://192.168.1.25:8080", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
	app := New(cfg, c, auth.New(cfg.APIKeys, false), router.New(c, keys), provider.NewClient(client, keys), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer client-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"content":"ok"`) {
		t.Fatalf("unexpected inference response: status=%d body=%s", response.Code, response.Body.String())
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/admin/usage", nil)
	adminRequest.SetBasicAuth("admin", "management-key")
	adminResponse := httptest.NewRecorder()
	app.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK || !strings.Contains(adminResponse.Body.String(), `"requests":1`) {
		t.Fatalf("unexpected usage response: status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}

	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/admin/usage", nil)
	deniedRequest.SetBasicAuth("admin", "wrong-password")
	deniedResponse := httptest.NewRecorder()
	app.ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusUnauthorized || !strings.Contains(deniedResponse.Body.String(), `"code":"invalid_admin_credentials"`) {
		t.Fatalf("invalid admin credentials were not rejected: status=%d body=%s", deniedResponse.Code, deniedResponse.Body.String())
	}

	legacyRequest := httptest.NewRequest(http.MethodGet, "/api/admin/usage", nil)
	legacyRequest.Header.Set("Authorization", "Bearer management-key")
	legacyResponse := httptest.NewRecorder()
	app.ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusOK {
		t.Fatalf("legacy management token was not accepted during migration: status=%d body=%s", legacyResponse.Code, legacyResponse.Body.String())
	}

	rulesRequest := httptest.NewRequest(http.MethodGet, "/api/admin/routing-rules", nil)
	rulesRequest.SetBasicAuth("admin", "management-key")
	rulesResponse := httptest.NewRecorder()
	app.ServeHTTP(rulesResponse, rulesRequest)
	if rulesResponse.Code != http.StatusOK || strings.TrimSpace(rulesResponse.Body.String()) != "[]" {
		t.Fatalf("empty routing rules must be returned as an array: status=%d body=%s", rulesResponse.Code, rulesResponse.Body.String())
	}
}

func TestProviderCanBeConfiguredWithoutStartupValidation(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "official", Type: "openai-compatible", BaseURL: "https://provider.example"}},
		Models:    []catalog.Model{{ID: "model", Name: "Model", Family: "test", Provider: "official", UpstreamModel: "upstream", Capabilities: []string{"chat"}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	protector, err := secret.LoadOrCreate(filepath.Join(directory, "master.key"), "")
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.OpenWithProtector(filepath.Join(directory, "gateway.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.SeedCatalog(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	runtimeCatalog, keys, err := database.RuntimeCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{APIKeys: []string{"client-key"}, AdminUsername: "admin", AdminPassword: "management-key", PublicURL: "http://192.168.1.25:8080", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
	app := New(cfg, runtimeCatalog, auth.New(cfg.APIKeys, false), router.New(runtimeCatalog, keys), provider.NewClient(nil), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/admin/providers", strings.NewReader(`{"id":"official","api_key":"provider-secret"}`))
	request.SetBasicAuth("admin", "management-key")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "provider-secret") || !strings.Contains(response.Body.String(), `"validation_status":"unverified"`) {
		t.Fatalf("unexpected provider response: status=%d body=%s", response.Code, response.Body.String())
	}

	modelsRequest := httptest.NewRequest(http.MethodGet, "/api/admin/models", nil)
	modelsRequest.SetBasicAuth("admin", "management-key")
	modelsResponse := httptest.NewRecorder()
	app.ServeHTTP(modelsResponse, modelsRequest)
	if modelsResponse.Code != http.StatusOK || !strings.Contains(modelsResponse.Body.String(), `"available":true`) {
		t.Fatalf("runtime router did not refresh: status=%d body=%s", modelsResponse.Code, modelsResponse.Body.String())
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	statusRequest.SetBasicAuth("admin", "management-key")
	statusResponse := httptest.NewRecorder()
	app.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"api_base_url":"http://192.168.1.25:8080/v1"`) {
		t.Fatalf("status did not expose configured API base URL: status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	policyRequest := httptest.NewRequest(http.MethodPost, "/api/admin/model-route-policies", strings.NewReader(`{"model_id":"model","strategy":"failover"}`))
	policyRequest.SetBasicAuth("admin", "management-key")
	policyResponse := httptest.NewRecorder()
	app.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"strategy":"failover"`) {
		t.Fatalf("model route policy was not saved: status=%d body=%s", policyResponse.Code, policyResponse.Body.String())
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/admin/model-route-policies", nil)
	listRequest.SetBasicAuth("admin", "management-key")
	listResponse := httptest.NewRecorder()
	app.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"model_id":"model"`) {
		t.Fatalf("model route policy was not listed: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/admin/model-route-policies/model", nil)
	deleteRequest.SetBasicAuth("admin", "management-key")
	deleteResponse := httptest.NewRecorder()
	app.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("model route policy was not deleted: status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestNormalizeModelCapabilities(t *testing.T) {
	model, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model"}, "custom-provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Capabilities) != len(defaultModelCapabilities) {
		t.Fatalf("expected every default capability, got %#v", model.Capabilities)
	}
	if model.Priority != 100 || model.Weight != 100 {
		t.Fatalf("unexpected default route policy: %#v", model)
	}

	model, err = normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Capabilities: []string{" Chat ", "chat", "responses"}}, "custom-provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Capabilities) != 2 || model.Capabilities[0] != "chat" || model.Capabilities[1] != "responses" {
		t.Fatalf("capabilities were not normalized: %#v", model.Capabilities)
	}

	if _, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Capabilities: []string{"unknown"}}, "custom-provider"); err == nil {
		t.Fatal("expected an unsupported capability to be rejected")
	}
	if _, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Priority: -1}, "custom-provider"); err == nil {
		t.Fatal("expected an invalid priority to be rejected")
	}
	priority, weight, err := normalizeRoutePolicy(0, 0)
	if err != nil || priority != 100 || weight != 100 {
		t.Fatalf("unexpected default route policy: priority=%d weight=%d error=%v", priority, weight, err)
	}
	if _, _, err := normalizeRoutePolicy(10, 10_001); err == nil {
		t.Fatal("expected an invalid route weight to be rejected")
	}
}

func TestInferenceFailsOverBetweenRoutesForSamePublicModel(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{
			{ID: "primary", Type: "openai-compatible", BaseURL: "https://primary.example"},
			{ID: "secondary", Type: "openai-compatible", BaseURL: "https://secondary.example"},
		},
		Models: []catalog.Model{
			{ID: "shared-model", Name: "Shared", Provider: "primary", UpstreamModel: "primary-model", Capabilities: []string{"chat"}, Priority: 10},
			{ID: "shared-model", Name: "Shared", Provider: "secondary", UpstreamModel: "secondary-model", Capabilities: []string{"chat"}, Priority: 20},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(filepath.Join(t.TempDir(), "failover.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	client := &http.Client{Transport: gatewayRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusUnauthorized, `{"error":"bad key"}`
		if r.URL.Host == "secondary.example" {
			status, body = http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"backup"}}]}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	keys := map[string]string{"primary": "primary-key", "secondary": "secondary-key"}
	cfg := config.Config{APIKeys: []string{"client-key"}, AdminUsername: "admin", AdminPassword: "password", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
	app := New(cfg, c, auth.New(cfg.APIKeys, false), router.New(c, keys), provider.NewClient(client), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"shared-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer client-key")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"content":"backup"`) {
		t.Fatalf("route failover failed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRoutingRuleAPIRefreshesRuntimeRouter(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "provider", Type: "openai-compatible", BaseURL: "https://provider.example"}},
		Models: []catalog.Model{
			{ID: "model-one", Name: "One", Provider: "provider", UpstreamModel: "one", Capabilities: []string{"chat"}},
			{ID: "model-two", Name: "Two", Provider: "provider", UpstreamModel: "two", Capabilities: []string{"chat"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	protector, err := secret.LoadOrCreate(filepath.Join(directory, "master.key"), "")
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.OpenWithProtector(filepath.Join(directory, "rules.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.SeedCatalog(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	connection, found, err := database.Provider(context.Background(), "provider")
	if err != nil || !found {
		t.Fatalf("provider not found: %#v %v", connection, err)
	}
	key := "provider-key"
	if err := database.SaveConnection(context.Background(), store.ConnectionUpdate{Provider: connection.Provider, Name: connection.Name, Official: true, Enabled: true, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	runtimeCatalog, keys, err := database.RuntimeCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runtimeRouter := router.New(runtimeCatalog, keys)
	cfg := config.Config{APIKeys: []string{"client-key"}, AdminUsername: "admin", AdminPassword: "password", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
	app := New(cfg, runtimeCatalog, auth.New(cfg.APIKeys, false), runtimeRouter, provider.NewClient(nil), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/routing-rules", strings.NewReader(`{"id":"smart-chat","name":"Smart chat","enabled":true,"members":[{"model_id":"model-one","priority":10,"weight":100},{"model_id":"model-two","priority":20,"weight":100}]}`))
	request.SetBasicAuth("admin", "password")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save routing rule failed: status=%d body=%s", response.Code, response.Body.String())
	}
	routes, err := runtimeRouter.Resolve("smart-chat", "chat")
	if err != nil || len(routes) != 2 || routes[0].Model.ID != "model-one" {
		t.Fatalf("runtime router was not refreshed: %#v %v", routes, err)
	}
}

type gatewayRoundTripFunc func(*http.Request) (*http.Response, error)

func (f gatewayRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

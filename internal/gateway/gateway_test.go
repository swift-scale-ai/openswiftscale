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
	if adminResponse.Code != http.StatusOK || !strings.Contains(adminResponse.Body.String(), `"requests":1`) || !strings.Contains(adminResponse.Body.String(), `"pagination":{"limit":100,"offset":0,"total":1}`) {
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
	if rulesResponse.Code != http.StatusNotFound {
		t.Fatalf("legacy routing rules API must not remain exposed: status=%d body=%s", rulesResponse.Code, rulesResponse.Body.String())
	}
}

func TestMultimodalInferenceEndpoints(t *testing.T) {
	tests := []struct {
		path, endpoint, capability, providerPath string
	}{
		{"/v1/images/generations", "image", "image", "/v1/images/generations"},
		{"/v1/rerank", "rerank", "rerank", "/v1/rerank"},
		{"/v1/videos", "video", "video", "/v1/videos"},
		{"/v1/audio/speech", "speech", "speech", "/v1/audio/speech"},
	}
	for _, test := range tests {
		t.Run(test.endpoint, func(t *testing.T) {
			providerConfig := catalog.Provider{ID: "mock", Type: "openai-compatible", BaseURL: "https://provider.example"}
			switch test.endpoint {
			case "image":
				providerConfig.ImagesPath = test.providerPath
			case "rerank":
				providerConfig.RerankPath = test.providerPath
			case "video":
				providerConfig.VideosPath = test.providerPath
			case "speech":
				providerConfig.SpeechPath = test.providerPath
			}
			c := &catalog.Catalog{Providers: []catalog.Provider{providerConfig}, Models: []catalog.Model{{ID: "public-model", Name: "Public Model", Provider: "mock", UpstreamModel: "upstream-model", Capabilities: []string{test.capability}}}}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			database, err := store.Open(filepath.Join(t.TempDir(), "multimodal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			client := &http.Client{Transport: gatewayRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != test.providerPath {
					t.Fatalf("unexpected provider path: %s", r.URL.Path)
				}
				payload, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(payload), `"model":"upstream-model"`) {
					t.Fatalf("model was not rewritten: %s", payload)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
			})}
			cfg := config.Config{APIKeys: []string{"client-key"}, AdminUsername: "admin", AdminPassword: "password", RateLimitRPM: 10, Concurrency: 2, MaxBodyBytes: 1 << 20}
			app := New(cfg, c, auth.New(cfg.APIKeys, false), router.New(c, map[string]string{"mock": "provider-key"}), provider.NewClient(client), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"model":"public-model"}`))
			request.Header.Set("Authorization", "Bearer client-key")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
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
	probeHTTPClient := &http.Client{Transport: gatewayRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || !strings.HasSuffix(request.URL.Path, "/v1/models") || request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Fatalf("unexpected provider probe: %s %s auth=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: request}, nil
	})}
	app := New(cfg, runtimeCatalog, auth.New(cfg.APIKeys, false), router.New(runtimeCatalog, keys), provider.NewClient(probeHTTPClient), database, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

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
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"api_base_url":"http://192.168.1.25:8080/v1"`) || !strings.Contains(statusResponse.Body.String(), `"local_api_base_url":"http://127.0.0.1:8080/v1"`) || !strings.Contains(statusResponse.Body.String(), `"lan_api_base_url":"http://192.168.1.25:8080/v1"`) {
		t.Fatalf("status did not expose configured API base URL: status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	probeRequest := httptest.NewRequest(http.MethodPost, "/api/admin/providers/official/validate", nil)
	probeRequest.SetBasicAuth("admin", "management-key")
	probeResponse := httptest.NewRecorder()
	app.ServeHTTP(probeResponse, probeRequest)
	if probeResponse.Code != http.StatusOK || !strings.Contains(probeResponse.Body.String(), `"valid":true`) {
		t.Fatalf("active provider validation failed: status=%d body=%s", probeResponse.Code, probeResponse.Body.String())
	}
	validated, found, err := database.Provider(context.Background(), "official")
	if err != nil || !found || validated.ValidationStatus != "valid" {
		t.Fatalf("provider validation was not persisted: %#v found=%v err=%v", validated, found, err)
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

	model, err = normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Capabilities: []string{" Chat ", "chat", "responses"}, InputPrice: 0.5, OutputPrice: 2, Currency: "usd"}, "custom-provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Capabilities) != 2 || model.Capabilities[0] != "chat" || model.Capabilities[1] != "responses" {
		t.Fatalf("capabilities were not normalized: %#v", model.Capabilities)
	}
	if model.Pricing.InputPerMillion != 0.5 || model.Pricing.OutputPerMillion != 2 || model.Pricing.Currency != "USD" {
		t.Fatalf("endpoint pricing was not normalized: %#v", model.Pricing)
	}
	model, err = normalizeModelInput(modelInput{ID: "media-model", UpstreamModel: "upstream-media", Capabilities: []string{"image", "rerank", "video", "speech", "transcription"}}, "custom-provider")
	if err != nil || len(model.Capabilities) != 5 {
		t.Fatalf("multimodal capabilities were rejected: %#v %v", model.Capabilities, err)
	}

	if _, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Capabilities: []string{"unknown"}}, "custom-provider"); err == nil {
		t.Fatal("expected an unsupported capability to be rejected")
	}
	if _, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", Priority: -1}, "custom-provider"); err == nil {
		t.Fatal("expected an invalid priority to be rejected")
	}
	if _, err := normalizeModelInput(modelInput{ID: "custom-model", UpstreamModel: "upstream-model", InputPrice: -1}, "custom-provider"); err == nil {
		t.Fatal("expected a negative price to be rejected")
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

func TestLegacyRoutingRuleAPIIsNotExposed(t *testing.T) {
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
	if response.Code != http.StatusMethodNotAllowed && response.Code != http.StatusNotFound {
		t.Fatalf("legacy routing rule API must not remain exposed: status=%d body=%s", response.Code, response.Body.String())
	}
}

type gatewayRoundTripFunc func(*http.Request) (*http.Response, error)

func (f gatewayRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

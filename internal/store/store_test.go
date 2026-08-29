package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/secret"
)

func TestRecordAndSummary(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	event := Usage{RequestID: "req-1", KeyID: "key-1", Model: "model", Provider: "provider", Endpoint: "chat", Status: 200, PromptTokens: 10, CompletionTokens: 20, CostUSD: 0.25, LatencyMS: 50, CreatedAt: time.Now()}
	if err := s.Record(ctx, event); err != nil {
		t.Fatal(err)
	}
	summary, err := s.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 1 || summary.PromptTokens != 10 || summary.CompletionTokens != 20 || summary.CostUSD != 0.25 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	recent, err := s.Recent(ctx, 10)
	if err != nil || len(recent) != 1 || recent[0].RequestID != "req-1" {
		t.Fatalf("unexpected recent usage: %#v, %v", recent, err)
	}
}

func TestRecentReturnsEmptyArrayWhenNoUsageExists(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "empty-usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	recent, err := s.Recent(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if recent == nil || len(recent) != 0 {
		t.Fatalf("expected a non-nil empty usage list, got %#v", recent)
	}
}

func TestProviderConfigurationIsEncryptedAndReloaded(t *testing.T) {
	directory := t.TempDir()
	protector, err := secret.LoadOrCreate(filepath.Join(directory, "master.key"), "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenWithProtector(filepath.Join(directory, "gateway.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := &catalog.Catalog{Providers: []catalog.Provider{{ID: "official", Type: "openai-compatible", BaseURL: "https://provider.example"}}, Families: []catalog.ModelFamily{{ID: "image", Name: "Image", Publisher: "Example", Provider: "official", Capabilities: []string{"image"}}}, Models: []catalog.Model{{ID: "model", Name: "Model", Family: "test", Provider: "official", UpstreamModel: "upstream", Capabilities: []string{"chat"}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedCatalog(ctx, c); err != nil {
		t.Fatal(err)
	}
	connection, found, err := s.Provider(ctx, "official")
	if err != nil || !found {
		t.Fatalf("missing seeded connection: %#v %v", connection, err)
	}
	key := "super-secret-provider-key"
	if err := s.SaveConnection(ctx, ConnectionUpdate{Provider: connection.Provider, Name: connection.Name, Official: true, Enabled: true, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT api_key_ciphertext FROM provider_connections WHERE id='official'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatal("provider credential was stored as plaintext")
	}
	runtimeCatalog, keys, err := s.RuntimeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimeCatalog.Models) != 1 || len(runtimeCatalog.Families) != 1 || runtimeCatalog.Families[0].ID != "image" || keys["official"] != key {
		t.Fatalf("unexpected runtime configuration: %#v %#v", runtimeCatalog, keys)
	}
	providers, err := s.Providers(ctx)
	if err != nil || len(providers) != 1 || !providers[0].HasAPIKey || strings.Contains(providers[0].MaskedAPIKey, key) {
		t.Fatalf("credential leaked through metadata: %#v %v", providers, err)
	}
}

func TestMultipleRoutesSharePublicModelAndSurviveConnectionDeletion(t *testing.T) {
	directory := t.TempDir()
	protector, err := secret.LoadOrCreate(filepath.Join(directory, "master.key"), "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenWithProtector(filepath.Join(directory, "gateway.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	save := func(providerID, upstream string, priority, weight int) {
		t.Helper()
		key := providerID + "-secret"
		p := catalog.Provider{ID: providerID, Type: "openai-compatible", BaseURL: "https://" + providerID + ".example", ChatPath: "/v1/chat/completions", ResponsesPath: "/v1/responses", EmbeddingsPath: "/v1/embeddings"}
		m := catalog.Model{ID: "shared-model", Name: "Shared model", Family: "test", Provider: providerID, UpstreamModel: upstream, Capabilities: []string{"chat"}, Priority: priority, Weight: weight, Pricing: catalog.Pricing{Currency: "USD"}}
		if err := s.SaveConnection(ctx, ConnectionUpdate{Provider: p, Name: providerID, Enabled: true, APIKey: &key, Models: []catalog.Model{m}}); err != nil {
			t.Fatal(err)
		}
	}
	save("route-one", "upstream-one", 10, 75)
	save("route-two", "upstream-two", 10, 25)

	runtimeCatalog, _, err := s.RuntimeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routes := runtimeCatalog.ModelsByID("shared-model")
	if len(routes) != 2 || routes[0].Priority != 10 || routes[0].Weight+routes[1].Weight != 100 {
		t.Fatalf("unexpected shared routes: %#v", routes)
	}
	connection, found, err := s.Provider(ctx, "route-two")
	if err != nil || !found {
		t.Fatalf("route provider not found: %#v %v", connection, err)
	}
	if err := s.SaveConnection(ctx, ConnectionUpdate{Provider: connection.Provider, Name: connection.Name, Enabled: true,
		Routes: []RoutePolicyUpdate{{ModelID: "shared-model", Priority: 5, Weight: 80}}}); err != nil {
		t.Fatal(err)
	}
	runtimeCatalog, _, err = s.RuntimeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range runtimeCatalog.ModelsByID("shared-model") {
		if route.Provider == "route-two" && (route.Priority != 5 || route.Weight != 80) {
			t.Fatalf("route policy was not updated: %#v", route)
		}
	}
	if err := s.DeleteConnection(ctx, "route-one"); err != nil {
		t.Fatal(err)
	}
	runtimeCatalog, _, err = s.RuntimeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routes = runtimeCatalog.ModelsByID("shared-model")
	if len(routes) != 1 || routes[0].Provider != "route-two" {
		t.Fatalf("remaining route was lost: %#v", routes)
	}
}

func TestSimpleModelRouteSettingsRoundTrip(t *testing.T) {
	directory := t.TempDir()
	protector, err := secret.LoadOrCreate(filepath.Join(directory, "master.key"), "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenWithProtector(filepath.Join(directory, "gateway.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, providerID := range []string{"route-one", "route-two"} {
		key := providerID + "-secret"
		p := catalog.Provider{ID: providerID, Type: "openai-compatible", BaseURL: "https://" + providerID + ".example"}
		m := catalog.Model{ID: "shared-model", Name: "Shared model", Family: "test", Provider: providerID, UpstreamModel: providerID, Capabilities: []string{"chat"}, Pricing: catalog.Pricing{Currency: "USD"}}
		if err := s.SaveConnection(ctx, ConnectionUpdate{Provider: p, Name: providerID, Enabled: true, APIKey: &key, Models: []catalog.Model{m}}); err != nil {
			t.Fatal(err)
		}
	}
	settings, found, err := s.ModelRouteSettings(ctx, "shared-model")
	if err != nil || !found || !settings.UsePlatformDefault || len(settings.Endpoints) != 2 {
		t.Fatalf("unexpected initial settings: %#v found=%v err=%v", settings, found, err)
	}
	settings.UsePlatformDefault = false
	settings.Endpoints[0], settings.Endpoints[1] = settings.Endpoints[1], settings.Endpoints[0]
	settings.Endpoints[0].Weight = 70
	settings.Endpoints[1].Weight = 30
	settings.Endpoints[1].Enabled = false
	if err := s.SaveModelRouteSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	updated, found, err := s.ModelRouteSettings(ctx, "shared-model")
	if err != nil || !found || updated.UsePlatformDefault || updated.Endpoints[0].ProviderID != "route-two" || updated.Endpoints[0].Priority != 10 || updated.Endpoints[0].Weight != 70 || updated.Endpoints[1].Enabled {
		t.Fatalf("manual settings were not persisted: %#v found=%v err=%v", updated, found, err)
	}
	runtimeCatalog, _, err := s.RuntimeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routes := runtimeCatalog.ModelsByID("shared-model")
	if len(routes) != 1 || !routes[0].ManualRouting || routes[0].RouteOrder != 10 || routes[0].Weight != 70 {
		t.Fatalf("manual routing metadata was not loaded: %#v", routes)
	}
	updated.UsePlatformDefault = true
	if err := s.SaveModelRouteSettings(ctx, updated); err != nil {
		t.Fatal(err)
	}
	defaults, _, err := s.ModelRouteSettings(ctx, "shared-model")
	if err != nil || !defaults.UsePlatformDefault || defaults.Endpoints[0].Priority != 100 || defaults.Endpoints[0].Weight != 100 {
		t.Fatalf("platform defaults were not restored: %#v err=%v", defaults, err)
	}
}

func TestMigrationBackfillsLegacyModelRoute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`
CREATE TABLE provider_connections (
 id TEXT PRIMARY KEY,name TEXT NOT NULL,type TEXT NOT NULL,base_url TEXT NOT NULL,chat_path TEXT NOT NULL,responses_path TEXT NOT NULL,
 embeddings_path TEXT NOT NULL,authentication TEXT NOT NULL,api_key_header TEXT NOT NULL DEFAULT '',api_key_ciphertext BLOB,api_key_nonce BLOB,
 encryption_key_version INTEGER NOT NULL DEFAULT 1,official INTEGER NOT NULL DEFAULT 0,enabled INTEGER NOT NULL DEFAULT 1,
 validation_status TEXT NOT NULL DEFAULT 'unverified',last_validated_at TEXT,last_error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE TABLE model_configs (
 id TEXT PRIMARY KEY,name TEXT NOT NULL,family TEXT NOT NULL,provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
 upstream_model TEXT NOT NULL,capabilities_json TEXT NOT NULL,context_window INTEGER NOT NULL DEFAULT 0,max_output_tokens INTEGER NOT NULL DEFAULT 0,
 input_per_million REAL NOT NULL DEFAULT 0,output_per_million REAL NOT NULL DEFAULT 0,currency TEXT NOT NULL DEFAULT 'USD',
 fallbacks_json TEXT NOT NULL DEFAULT '[]',metadata_json TEXT NOT NULL DEFAULT '{}',enabled INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
INSERT INTO provider_connections (id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,created_at,updated_at)
VALUES ('legacy','Legacy','openai-compatible','https://legacy.example','/v1/chat/completions','/v1/responses','/v1/embeddings','bearer',?,?);
INSERT INTO model_configs (id,name,family,provider_id,upstream_model,capabilities_json,created_at,updated_at)
VALUES ('legacy-model','Legacy model','test','legacy','legacy-upstream','["chat"]',?,?);`, now, now, now, now)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var priority, weight int
	if err := s.db.QueryRow(`SELECT priority,weight FROM model_routes WHERE model_id='legacy-model' AND provider_id='legacy'`).Scan(&priority, &weight); err != nil {
		t.Fatal(err)
	}
	if priority != 100 || weight != 100 {
		t.Fatalf("unexpected migrated route policy: priority=%d weight=%d", priority, weight)
	}
}

func TestRoutingRuleRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "routing-rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	emptyRules, err := s.RoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if emptyRules == nil || len(emptyRules) != 0 {
		t.Fatalf("empty routing rules must be encoded as an empty array: %#v", emptyRules)
	}
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "provider", Type: "openai-compatible", BaseURL: "https://provider.example"}},
		Models: []catalog.Model{
			{ID: "model-a", Name: "Model A", Family: "test", Provider: "provider", UpstreamModel: "a", Capabilities: []string{"chat"}},
			{ID: "model-b", Name: "Model B", Family: "test", Provider: "provider", UpstreamModel: "b", Capabilities: []string{"chat"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedCatalog(ctx, c); err != nil {
		t.Fatal(err)
	}
	rule := catalog.RoutingRule{ID: "smart-chat", Name: "Smart chat", Enabled: true, Members: []catalog.RoutingRuleMember{
		{ModelID: "model-a", Priority: 10, Weight: 70}, {ModelID: "model-b", Priority: 20, Weight: 30},
	}}
	if err := s.SaveRoutingRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	rules, err := s.RoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || len(rules[0].Members) != 2 || rules[0].Members[0].ModelID != "model-a" {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	if err := s.DeleteRoutingRule(ctx, "smart-chat"); err != nil {
		t.Fatal(err)
	}
	rules, err = s.RoutingRules(ctx)
	if err != nil || len(rules) != 0 {
		t.Fatalf("routing rule was not deleted: %#v %v", rules, err)
	}
}

func TestModelRoutePolicyRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "model-route-policies.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "provider", Type: "openai-compatible", BaseURL: "https://provider.example"}},
		Models:    []catalog.Model{{ID: "model-a", Name: "Model A", Family: "test", Provider: "provider", UpstreamModel: "a", Capabilities: []string{"chat"}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedCatalog(ctx, c); err != nil {
		t.Fatal(err)
	}
	policies, err := s.ModelRoutePolicies(ctx)
	if err != nil || policies == nil || len(policies) != 0 {
		t.Fatalf("expected an empty policy list: %#v %v", policies, err)
	}
	if err := s.SaveModelRoutePolicy(ctx, ModelRoutePolicy{ModelID: "model-a", Strategy: "failover"}); err != nil {
		t.Fatal(err)
	}
	policies, err = s.ModelRoutePolicies(ctx)
	if err != nil || len(policies) != 1 || policies[0].ModelID != "model-a" || policies[0].Strategy != "failover" {
		t.Fatalf("unexpected policies: %#v %v", policies, err)
	}
	if err := s.SaveModelRoutePolicy(ctx, ModelRoutePolicy{ModelID: "model-a", Strategy: "hybrid"}); err != nil {
		t.Fatal(err)
	}
	policies, err = s.ModelRoutePolicies(ctx)
	if err != nil || len(policies) != 1 || policies[0].Strategy != "hybrid" {
		t.Fatalf("policy update failed: %#v %v", policies, err)
	}
	if err := s.DeleteModelRoutePolicy(ctx, "model-a"); err != nil {
		t.Fatal(err)
	}
	policies, err = s.ModelRoutePolicies(ctx)
	if err != nil || len(policies) != 0 {
		t.Fatalf("policy was not deleted: %#v %v", policies, err)
	}
}

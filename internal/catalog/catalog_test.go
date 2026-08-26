package catalog

import "testing"

func TestValidateCatalog(t *testing.T) {
	c := Catalog{
		Providers: []Provider{{ID: "Example", BaseURL: "https://example.com/"}},
		Models:    []Model{{ID: "Example-Model", Provider: "example", UpstreamModel: "upstream", Capabilities: []string{"chat"}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Providers[0].ID != "example" || c.Models[0].ID != "example-model" {
		t.Fatalf("catalog identifiers were not normalized: %#v", c)
	}
	if !HasCapability(c.Models[0], "CHAT") {
		t.Fatal("capability lookup should be case insensitive")
	}
}

func TestRejectUnknownProvider(t *testing.T) {
	c := Catalog{Models: []Model{{ID: "model", Provider: "missing", UpstreamModel: "upstream"}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestAllowsMultipleProviderRoutesForPublicModel(t *testing.T) {
	c := Catalog{
		Providers: []Provider{{ID: "one", BaseURL: "https://one"}, {ID: "two", BaseURL: "https://two"}},
		Models: []Model{
			{ID: "shared", Provider: "one", UpstreamModel: "one-model", Capabilities: []string{"chat"}, Priority: 10, Weight: 70},
			{ID: "shared", Provider: "two", UpstreamModel: "two-model", Capabilities: []string{"chat"}, Priority: 10, Weight: 30},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if routes := c.ModelsByID("shared"); len(routes) != 2 {
		t.Fatalf("expected two routes, got %#v", routes)
	}
}

func TestBundledCatalogContainsCurrentModelIDs(t *testing.T) {
	c, err := Load("../../config/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	models := make(map[string]bool, len(c.Models))
	for _, model := range c.Models {
		models[model.ID] = true
	}
	for _, id := range []string{
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4.5",
		"deepseek-v4-pro", "deepseek-v4-flash", "glm-5.3", "hy3",
		"mimo-v2.5-pro", "mimo-v2.5", "minimax-m3",
		"nemotron-3.5-lightning", "nemotron-3-ultra",
		"qwen3.8-max", "qwen3.7-plus", "qwen3.7-flash",
		"gemini-3.1-pro-preview", "gemini-3.7-flash", "gemini-3.5-flash-lite",
	} {
		if !models[id] {
			t.Errorf("bundled catalog is missing %q", id)
		}
	}
}

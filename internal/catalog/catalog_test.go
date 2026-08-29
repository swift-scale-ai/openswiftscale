package catalog

import "testing"

func TestValidateCatalog(t *testing.T) {
	c := Catalog{
		Providers: []Provider{{ID: "Example", BaseURL: "https://example.com/"}},
		Families:  []ModelFamily{{ID: "Example Family", Name: "Example Family", Publisher: "Example", Provider: "example"}},
		Models:    []Model{{ID: "Example-Model", Provider: "example", UpstreamModel: "upstream", Capabilities: []string{"chat"}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Providers[0].ID != "example" || c.Families[0].ID != "example family" || c.Models[0].ID != "example-model" {
		t.Fatalf("catalog identifiers were not normalized: %#v", c)
	}
	if !HasCapability(c.Models[0], "CHAT") {
		t.Fatal("capability lookup should be case insensitive")
	}
}

func TestBundledCatalogContainsPublishedFamilies(t *testing.T) {
	c, err := Load("../../config/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	families := make(map[string]bool, len(c.Families))
	for _, family := range c.Families {
		families[family.Publisher+"/"+family.ID] = true
	}
	for _, key := range []string{
		"Alibaba/qwen", "Alibaba/wan", "Alibaba/happyhorse",
		"Anthropic/claude", "DeepSeek/deepseek", "Google/gemini", "Google/veo",
		"MiniMax/minimax", "MiniMax/hailuo", "NVIDIA/nemotron", "NVIDIA/cosmos",
		"OpenAI/gpt", "OpenAI/sora", "Tencent/hy", "Xiaomi/mimo",
		"Z.AI/glm", "Z.AI/cogvideox",
		"Meta/llama", "Meta/muse-spark", "Meta/muse-image", "Microsoft/phi", "Microsoft/mai-thinking", "Microsoft/mai-image",
		"Microsoft/mai-voice", "Microsoft/mai-transcribe", "Mistral AI/mistral", "Moonshot AI/kimi",
		"Poolside/laguna", "StepFun/step",
	} {
		if !families[key] {
			t.Errorf("bundled catalog is missing model family %q", key)
		}
	}
}

func TestBundledAdditionalFamiliesExposeOfficialAccess(t *testing.T) {
	c, err := Load("../../config/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	providers := make(map[string]bool, len(c.Providers))
	for _, provider := range c.Providers {
		providers[provider.ID] = true
	}
	for _, family := range c.Families {
		switch family.Publisher {
		case "Meta", "Microsoft", "Mistral AI", "Moonshot AI", "Poolside", "StepFun":
			if family.Provider == "" && family.OfficialEndpoint == "" {
				t.Errorf("%s/%s has no official access", family.Publisher, family.ID)
			}
			if family.Provider != "" && !providers[family.Provider] {
				t.Errorf("%s/%s references missing provider %q", family.Publisher, family.ID, family.Provider)
			}
		}
	}
}

func TestRejectUnknownProvider(t *testing.T) {
	c := Catalog{Models: []Model{{ID: "model", Provider: "missing", UpstreamModel: "upstream"}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestAllowsDiscoveryOnlyFamilyWithoutProvider(t *testing.T) {
	c := Catalog{Families: []ModelFamily{{ID: "llama", Name: "Llama", Publisher: "Meta"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsRouteableFamilyWithoutProvider(t *testing.T) {
	c := Catalog{Families: []ModelFamily{{ID: "llama", Name: "Llama", Publisher: "Meta", Routeable: true}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected routeable family without provider to fail validation")
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

package router

import (
	"errors"
	"testing"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
)

func TestResolveConfiguredFallback(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "primary", BaseURL: "https://primary"}, {ID: "backup", BaseURL: "https://backup"}},
		Models: []catalog.Model{
			{ID: "public", Provider: "primary", UpstreamModel: "primary-model", Capabilities: []string{"chat"}, Fallbacks: []string{"backup"}},
			{ID: "backup", Provider: "backup", UpstreamModel: "backup-model", Capabilities: []string{"chat"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	routes, err := New(c, map[string]string{"backup": "key"}).Resolve("public", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Model.ID != "backup" {
		t.Fatalf("unexpected routes: %#v", routes)
	}
}

func TestCapabilityMismatch(t *testing.T) {
	c := &catalog.Catalog{Providers: []catalog.Provider{{ID: "p", BaseURL: "https://p"}}, Models: []catalog.Model{{ID: "m", Provider: "p", UpstreamModel: "m", Capabilities: []string{"chat"}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	_, err := New(c, map[string]string{"p": "key"}).Resolve("m", "embeddings")
	if !errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("expected capability mismatch, got %v", err)
	}
}

func TestResolveOrdersRoutesByPriority(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "primary", BaseURL: "https://primary"}, {ID: "secondary", BaseURL: "https://secondary"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "secondary", UpstreamModel: "m2", Capabilities: []string{"chat"}, Priority: 200, Weight: 100},
			{ID: "shared", Provider: "primary", UpstreamModel: "m1", Capabilities: []string{"chat"}, Priority: 10, Weight: 100},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	routes, err := New(c, map[string]string{"primary": "key", "secondary": "key"}).Resolve("shared", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Provider.ID != "primary" || routes[1].Provider.ID != "secondary" {
		t.Fatalf("unexpected priority order: %#v", routes)
	}
}

func TestResolveDistributesEqualPriorityByWeight(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "a", BaseURL: "https://a"}, {ID: "b", BaseURL: "https://b"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "a", UpstreamModel: "a", Capabilities: []string{"chat"}, Priority: 100, Weight: 3},
			{ID: "shared", Provider: "b", UpstreamModel: "b", Capabilities: []string{"chat"}, Priority: 100, Weight: 1},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r := New(c, map[string]string{"a": "key", "b": "key"})
	selected := map[string]int{}
	for range 4 {
		routes, err := r.Resolve("shared", "chat")
		if err != nil {
			t.Fatal(err)
		}
		selected[routes[0].Provider.ID]++
	}
	if selected["a"] != 3 || selected["b"] != 1 {
		t.Fatalf("unexpected weighted distribution: %#v", selected)
	}
}

func TestResolveSkipsUnconfiguredHigherPriorityRoute(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "primary", BaseURL: "https://primary"}, {ID: "secondary", BaseURL: "https://secondary"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "primary", UpstreamModel: "m1", Capabilities: []string{"chat"}, Priority: 10},
			{ID: "shared", Provider: "secondary", UpstreamModel: "m2", Capabilities: []string{"chat"}, Priority: 20},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	routes, err := New(c, map[string]string{"secondary": "key"}).Resolve("shared", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Provider.ID != "secondary" {
		t.Fatalf("unexpected routes: %#v", routes)
	}
}

func TestResolveCrossModelRoutingRuleByPriority(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "one", BaseURL: "https://one"}, {ID: "two", BaseURL: "https://two"}},
		Models: []catalog.Model{
			{ID: "model-one", Provider: "one", UpstreamModel: "one", Capabilities: []string{"chat"}},
			{ID: "model-two", Provider: "two", UpstreamModel: "two", Capabilities: []string{"chat"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	rule := catalog.RoutingRule{ID: "smart-chat", Enabled: true, Members: []catalog.RoutingRuleMember{
		{ModelID: "model-two", Priority: 20, Weight: 100}, {ModelID: "model-one", Priority: 10, Weight: 100},
	}}
	routes, err := New(c, map[string]string{"one": "key", "two": "key"}, []catalog.RoutingRule{rule}).Resolve("smart-chat", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Model.ID != "model-one" || routes[1].Model.ID != "model-two" {
		t.Fatalf("unexpected cross-model route order: %#v", routes)
	}
}

func TestResolveCrossModelRoutingRuleByWeight(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "one", BaseURL: "https://one"}, {ID: "two", BaseURL: "https://two"}},
		Models: []catalog.Model{
			{ID: "model-one", Provider: "one", UpstreamModel: "one", Capabilities: []string{"chat"}},
			{ID: "model-two", Provider: "two", UpstreamModel: "two", Capabilities: []string{"chat"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	rule := catalog.RoutingRule{ID: "balanced", Enabled: true, Members: []catalog.RoutingRuleMember{
		{ModelID: "model-one", Priority: 10, Weight: 3}, {ModelID: "model-two", Priority: 10, Weight: 1},
	}}
	r := New(c, map[string]string{"one": "key", "two": "key"}, []catalog.RoutingRule{rule})
	selected := map[string]int{}
	for range 4 {
		routes, err := r.Resolve("balanced", "chat")
		if err != nil {
			t.Fatal(err)
		}
		selected[routes[0].Model.ID]++
	}
	if selected["model-one"] != 3 || selected["model-two"] != 1 {
		t.Fatalf("unexpected cross-model distribution: %#v", selected)
	}
}

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

func TestEndpointCapabilityAliases(t *testing.T) {
	for endpoint, advertised := range map[string]string{"rerank": "retrieval", "transcription": "asr", "speech": "tts"} {
		c := &catalog.Catalog{Providers: []catalog.Provider{{ID: "p", BaseURL: "https://provider.example"}}, Models: []catalog.Model{{ID: "m", Provider: "p", UpstreamModel: "upstream", Capabilities: []string{advertised}}}}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := New(c, map[string]string{"p": "key"}).Resolve("m", endpoint); err != nil {
			t.Fatalf("%s should accept %s capability: %v", endpoint, advertised, err)
		}
	}
}

func TestCatalogClonePreservesPublishedFamilies(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "p", BaseURL: "https://p"}},
		Families:  []catalog.ModelFamily{{ID: "video", Name: "Video", Publisher: "Publisher", Provider: "p", Capabilities: []string{"video"}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r := New(c, nil)
	got := r.Catalog()
	if len(got.Families) != 1 || got.Families[0].ID != "video" {
		t.Fatalf("published model families were dropped: %#v", got.Families)
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

func TestPlatformScoreOrdersSameModelEndpoints(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "expensive", BaseURL: "https://expensive"}, {ID: "efficient", BaseURL: "https://efficient"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "expensive", UpstreamModel: "m", Capabilities: []string{"chat"}, Priority: 10, PlatformScore: 55},
			{ID: "shared", Provider: "efficient", UpstreamModel: "m", Capabilities: []string{"chat"}, Priority: 200, PlatformScore: 92},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	routes, err := New(c, map[string]string{"expensive": "key", "efficient": "key"}).Resolve("shared", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Provider.ID != "efficient" {
		t.Fatalf("platform score was not used: %#v", routes)
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

func TestFailoverModeUsesDraggedOrder(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "a", BaseURL: "https://a"}, {ID: "b", BaseURL: "https://b"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "a", UpstreamModel: "a", Capabilities: []string{"chat"}, Priority: 10, RouteOrder: 10, Weight: 100, ManualRouting: true, RoutingMode: "failover"},
			{ID: "shared", Provider: "b", UpstreamModel: "b", Capabilities: []string{"chat"}, Priority: 20, RouteOrder: 20, Weight: 100, ManualRouting: true, RoutingMode: "failover"},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r := New(c, map[string]string{"a": "key", "b": "key"})
	for range 4 {
		routes, err := r.Resolve("shared", "chat")
		if err != nil {
			t.Fatal(err)
		}
		if routes[0].Provider.ID != "a" || routes[1].Provider.ID != "b" {
			t.Fatalf("failover order changed: %#v", routes)
		}
	}
}

func TestWeightedModeDistributesTrafficAndRetainsFailoverCandidates(t *testing.T) {
	c := &catalog.Catalog{
		Providers: []catalog.Provider{{ID: "a", BaseURL: "https://a"}, {ID: "b", BaseURL: "https://b"}},
		Models: []catalog.Model{
			{ID: "shared", Provider: "a", UpstreamModel: "a", Capabilities: []string{"chat"}, Priority: 100, Weight: 7, RoutingMode: "weighted"},
			{ID: "shared", Provider: "b", UpstreamModel: "b", Capabilities: []string{"chat"}, Priority: 100, Weight: 3, RoutingMode: "weighted"},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r := New(c, map[string]string{"a": "key", "b": "key"})
	selected := map[string]int{}
	for range 10 {
		routes, err := r.Resolve("shared", "chat")
		if err != nil {
			t.Fatal(err)
		}
		if len(routes) != 2 {
			t.Fatalf("weighted route dropped failover candidate: %#v", routes)
		}
		selected[routes[0].Provider.ID]++
	}
	if selected["a"] != 7 || selected["b"] != 3 {
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

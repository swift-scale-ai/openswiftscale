package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
)

var (
	ErrModelNotFound      = errors.New("model not found")
	ErrModelUnavailable   = errors.New("model provider is not configured")
	ErrCapabilityMismatch = errors.New("model does not support this endpoint")
)

type Route struct {
	Model    catalog.Model
	Provider catalog.Provider
	APIKey   string
}

type Router struct {
	mu      sync.RWMutex
	catalog *catalog.Catalog
	keys    map[string]string
	cursors sync.Map
}

func New(c *catalog.Catalog, keys map[string]string) *Router {
	return &Router{catalog: c, keys: keys}
}

func (r *Router) Replace(c *catalog.Catalog, keys map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalog = c
	r.keys = keys
	r.cursors = sync.Map{}
}

func (r *Router) Catalog() *catalog.Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneCatalog(r.catalog)
}

func (r *Router) Resolve(modelID, capability string) ([]Route, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	models := r.catalog.ModelsByID(modelID)
	if len(models) == 0 {
		return nil, ErrModelNotFound
	}
	hasCapability := false
	for _, model := range models {
		hasCapability = hasCapability || supportsEndpoint(model, capability)
	}
	if !hasCapability {
		return nil, ErrCapabilityMismatch
	}
	ordered := []string{models[0].ID}
	seenIDs := map[string]bool{models[0].ID: true}
	for _, model := range models {
		for _, fallbackID := range model.Fallbacks {
			fallbackID = strings.ToLower(strings.TrimSpace(fallbackID))
			if fallbackID != "" && !seenIDs[fallbackID] {
				ordered = append(ordered, fallbackID)
				seenIDs[fallbackID] = true
			}
		}
	}
	var routes []Route
	for _, id := range ordered {
		routes = append(routes, r.routesForModel(id, capability)...)
	}
	if len(routes) == 0 {
		return nil, ErrModelUnavailable
	}
	return routes, nil
}

func (r *Router) routesForModel(modelID, capability string) []Route {
	byPriority := make(map[int][]Route)
	priorities := make([]int, 0)
	for _, candidate := range r.catalog.ModelsByID(modelID) {
		if !supportsEndpoint(candidate, capability) {
			continue
		}
		provider, ok := r.catalog.ProviderByID(candidate.Provider)
		key := strings.TrimSpace(r.keys[candidate.Provider])
		if !ok || key == "" {
			continue
		}
		priority := candidate.Priority
		if candidate.RoutingMode == "weighted" {
			priority = 100
		} else if (candidate.RoutingMode == "platform" || candidate.RoutingMode == "") && candidate.PlatformScore > 0 {
			// Higher platform scores are attempted first. Convert the public
			// 0-100 score to the router's lower-is-better priority scale.
			priority = int((100-candidate.PlatformScore)*100) + 1
			candidate.Priority = priority
			candidate.RouteOrder = priority
			candidate.Weight = 100
		}
		if _, exists := byPriority[priority]; !exists {
			priorities = append(priorities, priority)
		}
		byPriority[priority] = append(byPriority[priority], Route{Model: candidate, Provider: provider, APIKey: key})
	}
	sort.Ints(priorities)
	var ordered []Route
	for _, priority := range priorities {
		group := byPriority[priority]
		sort.Slice(group, func(i, j int) bool {
			if group[i].Model.RouteOrder != group[j].Model.RouteOrder {
				return group[i].Model.RouteOrder < group[j].Model.RouteOrder
			}
			return group[i].Provider.ID < group[j].Provider.ID
		})
		selected := r.weightedIndex(modelID, priority, group)
		for offset := range group {
			ordered = append(ordered, group[(selected+offset)%len(group)])
		}
	}
	return ordered
}

func supportsEndpoint(model catalog.Model, endpoint string) bool {
	if catalog.HasCapability(model, endpoint) {
		return true
	}
	switch endpoint {
	case "rerank":
		return catalog.HasCapability(model, "retrieval")
	case "transcription":
		return catalog.HasCapability(model, "asr")
	case "speech":
		return catalog.HasCapability(model, "tts")
	default:
		return false
	}
}

func (r *Router) weightedIndex(modelID string, priority int, routes []Route) int {
	if len(routes) < 2 {
		return 0
	}
	total := uint64(0)
	for _, route := range routes {
		total += uint64(route.Model.Weight)
	}
	if total == 0 {
		return 0
	}
	position := r.nextPosition("model:"+modelID, priority, total)
	for index, route := range routes {
		weight := uint64(route.Model.Weight)
		if position < weight {
			return index
		}
		position -= weight
	}
	return 0
}

func (r *Router) nextPosition(key string, priority int, total uint64) uint64 {
	cursorKey := key + "\x00" + fmt.Sprint(priority)
	value, _ := r.cursors.LoadOrStore(cursorKey, &atomic.Uint64{})
	cursor := value.(*atomic.Uint64)
	position := cursor.Add(1) - 1
	return position % total
}

func (r *Router) Available(model catalog.Model) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.TrimSpace(r.keys[model.Provider]) != ""
}

func (r *Router) ModelAvailable(modelID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, model := range r.catalog.ModelsByID(modelID) {
		if strings.TrimSpace(r.keys[model.Provider]) != "" {
			return true
		}
	}
	return false
}

func (r *Router) RouteForProvider(providerID string) (Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	provider, ok := r.catalog.ProviderByID(providerID)
	if !ok {
		return Route{}, false
	}
	key := strings.TrimSpace(r.keys[providerID])
	if key == "" {
		return Route{}, false
	}
	for _, model := range r.catalog.Models {
		if model.Provider == providerID {
			return Route{Model: model, Provider: provider, APIKey: key}, true
		}
	}
	return Route{}, false
}

func cloneCatalog(source *catalog.Catalog) *catalog.Catalog {
	if source == nil {
		return &catalog.Catalog{}
	}
	out := &catalog.Catalog{Providers: append([]catalog.Provider(nil), source.Providers...), Families: append([]catalog.ModelFamily(nil), source.Families...), Models: append([]catalog.Model(nil), source.Models...)}
	for index := range out.Families {
		out.Families[index].Capabilities = append([]string(nil), out.Families[index].Capabilities...)
	}
	for index := range out.Models {
		out.Models[index].Capabilities = append([]string(nil), out.Models[index].Capabilities...)
		out.Models[index].Fallbacks = append([]string(nil), out.Models[index].Fallbacks...)
	}
	return out
}

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
	Model        catalog.Model
	Provider     catalog.Provider
	APIKey       string
	RuleID       string
	RulePriority int
	RuleWeight   int
}

type Router struct {
	mu      sync.RWMutex
	catalog *catalog.Catalog
	keys    map[string]string
	rules   map[string]catalog.RoutingRule
	cursors sync.Map
}

func New(c *catalog.Catalog, keys map[string]string, routingRules ...[]catalog.RoutingRule) *Router {
	r := &Router{catalog: c, keys: keys}
	if len(routingRules) > 0 {
		r.setRules(routingRules[0])
	}
	return r
}

func (r *Router) Replace(c *catalog.Catalog, keys map[string]string, routingRules ...[]catalog.RoutingRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalog = c
	r.keys = keys
	if len(routingRules) > 0 {
		r.setRules(routingRules[0])
	}
	r.cursors = sync.Map{}
}

func (r *Router) setRules(rules []catalog.RoutingRule) {
	r.rules = make(map[string]catalog.RoutingRule, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			r.rules[strings.ToLower(strings.TrimSpace(rule.ID))] = rule
		}
	}
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
		rule, ok := r.rules[strings.ToLower(strings.TrimSpace(modelID))]
		if !ok {
			return nil, ErrModelNotFound
		}
		return r.resolveRule(rule, capability)
	}
	hasCapability := false
	for _, model := range models {
		hasCapability = hasCapability || catalog.HasCapability(model, capability)
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

func (r *Router) resolveRule(rule catalog.RoutingRule, capability string) ([]Route, error) {
	byPriority := make(map[int][]catalog.RoutingRuleMember)
	memberRoutes := make(map[string][]Route)
	priorities := make([]int, 0)
	hasCapability := false
	for _, member := range rule.Members {
		memberHasCapability := false
		for _, model := range r.catalog.ModelsByID(member.ModelID) {
			memberHasCapability = memberHasCapability || catalog.HasCapability(model, capability)
		}
		hasCapability = hasCapability || memberHasCapability
		if !memberHasCapability {
			continue
		}
		resolved := r.routesForModel(member.ModelID, capability)
		if len(resolved) == 0 {
			continue
		}
		for index := range resolved {
			resolved[index].RuleID = rule.ID
			resolved[index].RulePriority = member.Priority
			resolved[index].RuleWeight = member.Weight
		}
		memberRoutes[member.ModelID] = resolved
		if _, exists := byPriority[member.Priority]; !exists {
			priorities = append(priorities, member.Priority)
		}
		byPriority[member.Priority] = append(byPriority[member.Priority], member)
	}
	if !hasCapability {
		return nil, ErrCapabilityMismatch
	}
	sort.Ints(priorities)
	var routes []Route
	for _, priority := range priorities {
		members := byPriority[priority]
		sort.Slice(members, func(i, j int) bool { return members[i].ModelID < members[j].ModelID })
		selected := r.weightedMemberIndex(rule.ID, priority, members)
		for offset := range members {
			member := members[(selected+offset)%len(members)]
			routes = append(routes, memberRoutes[member.ModelID]...)
		}
	}
	if len(routes) == 0 {
		return nil, ErrModelUnavailable
	}
	return deduplicateRoutes(routes), nil
}

func (r *Router) weightedMemberIndex(ruleID string, priority int, members []catalog.RoutingRuleMember) int {
	if len(members) < 2 {
		return 0
	}
	total := uint64(0)
	for _, member := range members {
		total += uint64(member.Weight)
	}
	if total == 0 {
		return 0
	}
	position := r.nextPosition("rule:"+ruleID, priority, total)
	for index, member := range members {
		weight := uint64(member.Weight)
		if position < weight {
			return index
		}
		position -= weight
	}
	return 0
}

func (r *Router) routesForModel(modelID, capability string) []Route {
	byPriority := make(map[int][]Route)
	priorities := make([]int, 0)
	for _, candidate := range r.catalog.ModelsByID(modelID) {
		if !catalog.HasCapability(candidate, capability) {
			continue
		}
		provider, ok := r.catalog.ProviderByID(candidate.Provider)
		key := strings.TrimSpace(r.keys[candidate.Provider])
		if !ok || key == "" {
			continue
		}
		if _, exists := byPriority[candidate.Priority]; !exists {
			priorities = append(priorities, candidate.Priority)
		}
		byPriority[candidate.Priority] = append(byPriority[candidate.Priority], Route{Model: candidate, Provider: provider, APIKey: key})
	}
	sort.Ints(priorities)
	var ordered []Route
	for _, priority := range priorities {
		group := byPriority[priority]
		sort.Slice(group, func(i, j int) bool { return group[i].Provider.ID < group[j].Provider.ID })
		selected := r.weightedIndex(modelID, priority, group)
		for offset := range group {
			ordered = append(ordered, group[(selected+offset)%len(group)])
		}
	}
	return ordered
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

func deduplicateRoutes(routes []Route) []Route {
	seen := make(map[string]bool)
	out := make([]Route, 0, len(routes))
	for _, route := range routes {
		key := route.Model.ID + "\x00" + route.Provider.ID
		if !seen[key] {
			seen[key] = true
			out = append(out, route)
		}
	}
	return out
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

func (r *Router) RuleAvailable(ruleID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rule, ok := r.rules[strings.ToLower(strings.TrimSpace(ruleID))]
	if !ok {
		return false
	}
	for _, member := range rule.Members {
		for _, model := range r.catalog.ModelsByID(member.ModelID) {
			if strings.TrimSpace(r.keys[model.Provider]) != "" {
				return true
			}
		}
	}
	return false
}

func cloneCatalog(source *catalog.Catalog) *catalog.Catalog {
	if source == nil {
		return &catalog.Catalog{}
	}
	out := &catalog.Catalog{Providers: append([]catalog.Provider(nil), source.Providers...), Models: append([]catalog.Model(nil), source.Models...)}
	for index := range out.Models {
		out.Models[index].Capabilities = append([]string(nil), out.Models[index].Capabilities...)
		out.Models[index].Fallbacks = append([]string(nil), out.Models[index].Fallbacks...)
	}
	return out
}

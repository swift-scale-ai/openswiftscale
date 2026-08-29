package catalog

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	Providers []Provider    `yaml:"providers" json:"providers"`
	Families  []ModelFamily `yaml:"families,omitempty" json:"families,omitempty"`
	Models    []Model       `yaml:"models" json:"models"`
}

type ModelFamily struct {
	ID               string   `yaml:"id" json:"id"`
	Name             string   `yaml:"name" json:"name"`
	Publisher        string   `yaml:"publisher" json:"publisher"`
	Provider         string   `yaml:"provider" json:"provider"`
	Capabilities     []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Routeable        bool     `yaml:"routeable" json:"routeable"`
	OfficialAccess   string   `yaml:"official_access,omitempty" json:"official_access,omitempty"`
	OfficialEndpoint string   `yaml:"official_endpoint,omitempty" json:"official_endpoint,omitempty"`
}

type Provider struct {
	ID              string `yaml:"id" json:"id"`
	Type            string `yaml:"type" json:"type"`
	BaseURL         string `yaml:"base_url" json:"base_url"`
	ChatPath        string `yaml:"chat_path,omitempty" json:"chat_path,omitempty"`
	ResponsesPath   string `yaml:"responses_path,omitempty" json:"responses_path,omitempty"`
	EmbeddingsPath  string `yaml:"embeddings_path,omitempty" json:"embeddings_path,omitempty"`
	Authentication  string `yaml:"authentication,omitempty" json:"authentication,omitempty"`
	APIKeyHeader    string `yaml:"api_key_header,omitempty" json:"api_key_header,omitempty"`
	APIKeyQueryName string `yaml:"api_key_query_name,omitempty" json:"api_key_query_name,omitempty"`
}

type Model struct {
	ID              string            `yaml:"id" json:"id"`
	Name            string            `yaml:"name" json:"name"`
	Family          string            `yaml:"family" json:"family"`
	Provider        string            `yaml:"provider" json:"provider"`
	UpstreamModel   string            `yaml:"upstream_model" json:"upstream_model"`
	Capabilities    []string          `yaml:"capabilities" json:"capabilities"`
	ContextWindow   int               `yaml:"context_window" json:"context_window"`
	MaxOutputTokens int               `yaml:"max_output_tokens" json:"max_output_tokens"`
	Pricing         Pricing           `yaml:"pricing" json:"pricing"`
	Fallbacks       []string          `yaml:"fallbacks,omitempty" json:"fallbacks,omitempty"`
	Metadata        map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Priority        int               `yaml:"priority,omitempty" json:"priority"`
	Weight          int               `yaml:"weight,omitempty" json:"weight"`
	RouteOrder      int               `yaml:"-" json:"route_order,omitempty"`
	ManualRouting   bool              `yaml:"-" json:"-"`
}

type Pricing struct {
	InputPerMillion  float64 `yaml:"input_per_million" json:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million" json:"output_per_million"`
	Currency         string  `yaml:"currency" json:"currency"`
}

type RoutingRule struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Enabled bool                `json:"enabled"`
	Members []RoutingRuleMember `json:"members"`
}

type RoutingRuleMember struct {
	ModelID  string `json:"model_id"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
}

func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Catalog) Validate() error {
	providers := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		p.ID = strings.ToLower(strings.TrimSpace(p.ID))
		p.Type = strings.ToLower(strings.TrimSpace(p.Type))
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if p.ID == "" || p.BaseURL == "" {
			return fmt.Errorf("catalog provider id and base_url are required")
		}
		if p.Type == "" {
			p.Type = "openai-compatible"
		}
		if p.ChatPath == "" {
			p.ChatPath = "/v1/chat/completions"
		}
		if p.ResponsesPath == "" {
			p.ResponsesPath = "/v1/responses"
		}
		if p.EmbeddingsPath == "" {
			p.EmbeddingsPath = "/v1/embeddings"
		}
		providers[p.ID] = true
	}
	families := map[string]bool{}
	for i := range c.Families {
		family := &c.Families[i]
		family.ID = strings.ToLower(strings.TrimSpace(family.ID))
		family.Name = strings.TrimSpace(family.Name)
		family.Publisher = strings.TrimSpace(family.Publisher)
		family.Provider = strings.ToLower(strings.TrimSpace(family.Provider))
		family.OfficialAccess = strings.ToLower(strings.TrimSpace(family.OfficialAccess))
		family.OfficialEndpoint = strings.TrimRight(strings.TrimSpace(family.OfficialEndpoint), "/")
		if family.ID == "" || family.Name == "" || family.Publisher == "" {
			return fmt.Errorf("catalog family id, name and publisher are required")
		}
		if family.Routeable && family.Provider == "" {
			return fmt.Errorf("routeable model family %q requires a provider", family.ID)
		}
		if family.Provider != "" && !providers[family.Provider] {
			return fmt.Errorf("model family %q references unknown provider %q", family.ID, family.Provider)
		}
		if family.Provider == "" && family.OfficialEndpoint != "" && family.OfficialAccess == "" {
			return fmt.Errorf("model family %q with an official endpoint requires official_access", family.ID)
		}
		if family.OfficialAccess != "" && family.OfficialAccess != "deployment" && family.OfficialAccess != "self-hosted" {
			return fmt.Errorf("model family %q has unsupported official_access %q", family.ID, family.OfficialAccess)
		}
		key := strings.ToLower(family.Publisher) + "\x00" + family.ID
		if families[key] {
			return fmt.Errorf("duplicate model family %q for publisher %q", family.ID, family.Publisher)
		}
		families[key] = true
	}
	seen := map[string]bool{}
	for i := range c.Models {
		m := &c.Models[i]
		m.ID = strings.ToLower(strings.TrimSpace(m.ID))
		m.Provider = strings.ToLower(strings.TrimSpace(m.Provider))
		if m.ID == "" || m.Provider == "" || m.UpstreamModel == "" {
			return fmt.Errorf("catalog model id, provider and upstream_model are required")
		}
		if !providers[m.Provider] {
			return fmt.Errorf("model %q references unknown provider %q", m.ID, m.Provider)
		}
		routeKey := m.ID + "\x00" + m.Provider
		if seen[routeKey] {
			return fmt.Errorf("duplicate route for model %q and provider %q", m.ID, m.Provider)
		}
		seen[routeKey] = true
		if m.Priority < 0 {
			return fmt.Errorf("model %q priority must be non-negative", m.ID)
		}
		if m.Priority == 0 {
			m.Priority = 100
		}
		if m.Weight < 0 {
			return fmt.Errorf("model %q weight must be non-negative", m.ID)
		}
		if m.Weight == 0 {
			m.Weight = 100
		}
		if m.Pricing.Currency == "" {
			m.Pricing.Currency = "USD"
		}
	}
	return nil
}

func (c *Catalog) SortedFamilies() []ModelFamily {
	out := append([]ModelFamily(nil), c.Families...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Publisher != out[j].Publisher {
			return out[i].Publisher < out[j].Publisher
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (c *Catalog) ProviderByID(id string) (Provider, bool) {
	for _, p := range c.Providers {
		if p.ID == strings.ToLower(strings.TrimSpace(id)) {
			return p, true
		}
	}
	return Provider{}, false
}

func (c *Catalog) ModelByID(id string) (Model, bool) {
	for _, m := range c.Models {
		if m.ID == strings.ToLower(strings.TrimSpace(id)) {
			return m, true
		}
	}
	return Model{}, false
}

func (c *Catalog) ModelsByID(id string) []Model {
	id = strings.ToLower(strings.TrimSpace(id))
	out := make([]Model, 0)
	for _, m := range c.Models {
		if m.ID == id {
			out = append(out, m)
		}
	}
	return out
}

func (c *Catalog) SortedModels() []Model {
	out := append([]Model(nil), c.Models...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func HasCapability(m Model, capability string) bool {
	for _, item := range m.Capabilities {
		if strings.EqualFold(strings.TrimSpace(item), capability) {
			return true
		}
	}
	return false
}

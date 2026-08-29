package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/secret"
	_ "modernc.org/sqlite"
)

type Usage struct {
	RequestID        string    `json:"request_id"`
	KeyID            string    `json:"key_id"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider"`
	Endpoint         string    `json:"endpoint"`
	Status           int       `json:"status"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	LatencyMS        int64     `json:"latency_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

type Summary struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	AverageLatencyMS float64 `json:"average_latency_ms"`
}

type Store struct {
	db        *sql.DB
	protector *secret.Protector
	families  []catalog.ModelFamily
}

type ProviderConnection struct {
	catalog.Provider
	Name             string    `json:"name"`
	Official         bool      `json:"official"`
	ModelCategory    string    `json:"model_category,omitempty"`
	Enabled          bool      `json:"enabled"`
	HasAPIKey        bool      `json:"has_api_key"`
	MaskedAPIKey     string    `json:"masked_api_key,omitempty"`
	ValidationStatus string    `json:"validation_status"`
	LastValidatedAt  time.Time `json:"last_validated_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ConnectionUpdate struct {
	Provider      catalog.Provider
	Name          string
	Official      bool
	ModelCategory string
	Enabled       bool
	APIKey        *string
	Models        []catalog.Model
	Routes        []RoutePolicyUpdate
}

type RoutePolicyUpdate struct {
	ModelID  string
	Priority int
	Weight   int
}

type ModelRoutePolicy struct {
	ModelID   string    `json:"model_id"`
	Strategy  string    `json:"strategy"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ModelRouteEndpoint struct {
	ProviderID  string  `json:"provider_id"`
	Name        string  `json:"name"`
	BaseURL     string  `json:"base_url"`
	Official    bool    `json:"official"`
	Enabled     bool    `json:"enabled"`
	Priority    int     `json:"priority"`
	Weight      int     `json:"weight"`
	InputPrice  float64 `json:"input_price"`
	OutputPrice float64 `json:"output_price"`
}

type ModelRouteSettings struct {
	ModelID            string               `json:"model_id"`
	UsePlatformDefault bool                 `json:"use_platform_default"`
	Endpoints          []ModelRouteEndpoint `json:"endpoints"`
}

func Open(path string) (*Store, error) {
	return OpenWithProtector(path, nil)
}

func OpenWithProtector(path string, protector *secret.Protector) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, protector: protector}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS usage_events (
  request_id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL,
  model TEXT NOT NULL,
  provider TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  status INTEGER NOT NULL,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd REAL NOT NULL DEFAULT 0,
  latency_ms INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS usage_created_at_idx ON usage_events(created_at DESC);
CREATE INDEX IF NOT EXISTS usage_model_idx ON usage_events(model);
CREATE TABLE IF NOT EXISTS api_users (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES api_users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash BLOB NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_used_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS provider_connections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  base_url TEXT NOT NULL,
  chat_path TEXT NOT NULL,
  responses_path TEXT NOT NULL,
  embeddings_path TEXT NOT NULL,
  authentication TEXT NOT NULL,
  api_key_header TEXT NOT NULL DEFAULT '',
  api_key_ciphertext BLOB,
  api_key_nonce BLOB,
  encryption_key_version INTEGER NOT NULL DEFAULT 1,
  official INTEGER NOT NULL DEFAULT 0,
  model_category TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  validation_status TEXT NOT NULL DEFAULT 'unverified',
  last_validated_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_configs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  family TEXT NOT NULL,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  capabilities_json TEXT NOT NULL,
  context_window INTEGER NOT NULL DEFAULT 0,
  max_output_tokens INTEGER NOT NULL DEFAULT 0,
  input_per_million REAL NOT NULL DEFAULT 0,
  output_per_million REAL NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  fallbacks_json TEXT NOT NULL DEFAULT '[]',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS model_provider_idx ON model_configs(provider_id);
CREATE TABLE IF NOT EXISTS model_routes (
  model_id TEXT NOT NULL REFERENCES model_configs(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  capabilities_json TEXT NOT NULL,
  context_window INTEGER NOT NULL DEFAULT 0,
  max_output_tokens INTEGER NOT NULL DEFAULT 0,
  input_per_million REAL NOT NULL DEFAULT 0,
  output_per_million REAL NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 100,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(model_id, provider_id)
);
CREATE INDEX IF NOT EXISTS model_route_provider_idx ON model_routes(provider_id);
CREATE INDEX IF NOT EXISTS model_route_selection_idx ON model_routes(model_id, priority, enabled);
CREATE TABLE IF NOT EXISTS model_route_policies (
  model_id TEXT PRIMARY KEY REFERENCES model_configs(id) ON DELETE CASCADE,
  strategy TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_route_preferences (
  model_id TEXT PRIMARY KEY REFERENCES model_configs(id) ON DELETE CASCADE,
  use_platform_default INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS routing_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS routing_rule_members (
  rule_id TEXT NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
  model_id TEXT NOT NULL REFERENCES model_configs(id) ON DELETE CASCADE,
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 100,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(rule_id, model_id)
);
CREATE INDEX IF NOT EXISTS routing_rule_member_order_idx ON routing_rule_members(rule_id, priority, model_id);
INSERT OR IGNORE INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,100,100,enabled,created_at,updated_at
FROM model_configs;
`)
	if err != nil {
		return err
	}
	return s.ensureColumn(ctx, "provider_connections", "model_category", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func (s *Store) SeedCatalog(ctx context.Context, c *catalog.Catalog) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range c.Providers {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO provider_connections
(id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,api_key_header,official,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,1,1,?,?)`, p.ID, providerDisplayName(p.ID), p.Type, p.BaseURL, p.ChatPath, p.ResponsesPath, p.EmbeddingsPath, p.Authentication, p.APIKeyHeader, now, now)
		if err != nil {
			return err
		}
	}
	seededRoutes := make(map[string]bool, len(c.Models))
	for _, m := range c.Models {
		seededRoutes[m.ID+"\x00"+m.Provider] = true
		if err := insertModel(ctx, tx, m, true, true, now); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.model_id,r.provider_id FROM model_routes r
JOIN provider_connections p ON p.id=r.provider_id
JOIN model_configs m ON m.id=r.model_id
WHERE p.official=1 AND m.metadata_json NOT LIKE '%"demo":true%'`)
	if err != nil {
		return err
	}
	type routeKey struct{ modelID, providerID string }
	var obsolete []routeKey
	for rows.Next() {
		var item routeKey
		if err := rows.Scan(&item.modelID, &item.providerID); err != nil {
			_ = rows.Close()
			return err
		}
		if !seededRoutes[item.modelID+"\x00"+item.providerID] {
			obsolete = append(obsolete, item)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range obsolete {
		if _, err := tx.ExecContext(ctx, `DELETE FROM model_routes WHERE model_id=? AND provider_id=?`, item.modelID, item.providerID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM model_configs WHERE NOT EXISTS (SELECT 1 FROM model_routes r WHERE r.model_id=model_configs.id)`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.families = append([]catalog.ModelFamily(nil), c.Families...)
	for index := range s.families {
		s.families[index].Capabilities = append([]string(nil), s.families[index].Capabilities...)
	}
	return nil
}

func insertModel(ctx context.Context, tx *sql.Tx, m catalog.Model, updateCanonical, preserveRoutePolicy bool, now string) error {
	capabilities, _ := json.Marshal(m.Capabilities)
	fallbacks, _ := json.Marshal(m.Fallbacks)
	metadata, _ := json.Marshal(m.Metadata)
	command := `INSERT INTO model_configs
(id,name,family,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,fallbacks_json,metadata_json,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)`
	if updateCanonical {
		command += ` ON CONFLICT(id) DO UPDATE SET name=excluded.name,family=excluded.family,provider_id=excluded.provider_id,
upstream_model=excluded.upstream_model,capabilities_json=excluded.capabilities_json,context_window=excluded.context_window,
max_output_tokens=excluded.max_output_tokens,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,
currency=excluded.currency,fallbacks_json=excluded.fallbacks_json,metadata_json=excluded.metadata_json,enabled=1,updated_at=excluded.updated_at`
	} else {
		command = strings.Replace(command, "INSERT INTO", "INSERT OR IGNORE INTO", 1)
	}
	if _, err := tx.ExecContext(ctx, command, m.ID, m.Name, m.Family, m.Provider, m.UpstreamModel, string(capabilities), m.ContextWindow,
		m.MaxOutputTokens, m.Pricing.InputPerMillion, m.Pricing.OutputPerMillion, m.Pricing.Currency, string(fallbacks), string(metadata), now, now); err != nil {
		return err
	}
	routeCommand := `INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(model_id,provider_id) DO UPDATE SET
upstream_model=excluded.upstream_model,capabilities_json=excluded.capabilities_json,context_window=excluded.context_window,
max_output_tokens=excluded.max_output_tokens,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,
currency=excluded.currency,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at`
	if preserveRoutePolicy {
		routeCommand = `INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(model_id,provider_id) DO UPDATE SET
upstream_model=excluded.upstream_model,capabilities_json=excluded.capabilities_json,context_window=excluded.context_window,
max_output_tokens=excluded.max_output_tokens,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,
currency=excluded.currency,updated_at=excluded.updated_at`
	}
	_, err := tx.ExecContext(ctx, routeCommand,
		m.ID, m.Provider, m.UpstreamModel, string(capabilities), m.ContextWindow, m.MaxOutputTokens,
		m.Pricing.InputPerMillion, m.Pricing.OutputPerMillion, m.Pricing.Currency, m.Priority, m.Weight, now, now)
	return err
}

func (s *Store) RuntimeCatalog(ctx context.Context) (*catalog.Catalog, map[string]string, error) {
	// Keep disabled providers in the runtime catalog as discovery metadata so
	// their published model families remain visible. Disabled connections do
	// not contribute model routes or decrypted routing keys below.
	connections, err := s.providerRows(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	c := &catalog.Catalog{Families: append([]catalog.ModelFamily(nil), s.families...)}
	for index := range c.Families {
		c.Families[index].Capabilities = append([]string(nil), c.Families[index].Capabilities...)
	}
	keys := make(map[string]string, len(connections))
	for _, row := range connections {
		c.Providers = append(c.Providers, row.connection.Provider)
		if row.connection.Enabled && len(row.ciphertext) > 0 {
			if s.protector == nil {
				return nil, nil, fmt.Errorf("provider %q is encrypted but no master key is available", row.connection.ID)
			}
			key, err := s.protector.Decrypt(row.ciphertext, row.nonce, []byte("provider:"+row.connection.ID))
			if err != nil {
				return nil, nil, fmt.Errorf("provider %q: %w", row.connection.ID, err)
			}
			keys[row.connection.ID] = key
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.name,m.family,r.provider_id,r.upstream_model,r.capabilities_json,r.context_window,
r.max_output_tokens,r.input_per_million,r.output_per_million,r.currency,m.fallbacks_json,m.metadata_json,r.priority,r.weight,COALESCE(pref.use_platform_default,1)
FROM model_routes r JOIN model_configs m ON m.id=r.model_id JOIN provider_connections p ON p.id=r.provider_id
LEFT JOIN model_route_preferences pref ON pref.model_id=r.model_id
WHERE m.enabled=1 AND r.enabled=1 AND p.enabled=1 ORDER BY m.id,r.priority,r.provider_id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m catalog.Model
		var capabilities, fallbacks, metadata string
		var usePlatformDefault int
		if err := rows.Scan(&m.ID, &m.Name, &m.Family, &m.Provider, &m.UpstreamModel, &capabilities, &m.ContextWindow,
			&m.MaxOutputTokens, &m.Pricing.InputPerMillion, &m.Pricing.OutputPerMillion, &m.Pricing.Currency, &fallbacks, &metadata,
			&m.Priority, &m.Weight, &usePlatformDefault); err != nil {
			return nil, nil, err
		}
		m.RouteOrder = m.Priority
		m.ManualRouting = usePlatformDefault == 0
		_ = json.Unmarshal([]byte(capabilities), &m.Capabilities)
		_ = json.Unmarshal([]byte(fallbacks), &m.Fallbacks)
		_ = json.Unmarshal([]byte(metadata), &m.Metadata)
		c.Models = append(c.Models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	return c, keys, nil
}

type providerRow struct {
	connection ProviderConnection
	ciphertext []byte
	nonce      []byte
}

func (s *Store) providerRows(ctx context.Context, enabledOnly bool) ([]providerRow, error) {
	query := `SELECT id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,api_key_header,
api_key_ciphertext,api_key_nonce,official,model_category,enabled,validation_status,last_validated_at,last_error,created_at,updated_at FROM provider_connections`
	if enabledOnly {
		query += " WHERE enabled=1"
	}
	query += " ORDER BY official DESC,name"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []providerRow
	for rows.Next() {
		var row providerRow
		var official, enabled int
		var lastValidated sql.NullString
		var created, updated string
		p := &row.connection
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.ChatPath, &p.ResponsesPath, &p.EmbeddingsPath,
			&p.Authentication, &p.APIKeyHeader, &row.ciphertext, &row.nonce, &official, &p.ModelCategory, &enabled, &p.ValidationStatus,
			&lastValidated, &p.LastError, &created, &updated); err != nil {
			return nil, err
		}
		p.Official, p.Enabled, p.HasAPIKey = official == 1, enabled == 1, len(row.ciphertext) > 0
		if p.HasAPIKey {
			p.MaskedAPIKey = "••••••••"
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		if lastValidated.Valid {
			p.LastValidatedAt, _ = time.Parse(time.RFC3339Nano, lastValidated.String)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) Providers(ctx context.Context) ([]ProviderConnection, error) {
	rows, err := s.providerRows(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderConnection, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.connection)
	}
	return out, nil
}

func (s *Store) Provider(ctx context.Context, id string) (ProviderConnection, bool, error) {
	rows, err := s.providerRows(ctx, false)
	if err != nil {
		return ProviderConnection{}, false, err
	}
	for _, row := range rows {
		if row.connection.ID == strings.ToLower(strings.TrimSpace(id)) {
			return row.connection, true, nil
		}
	}
	return ProviderConnection{}, false, nil
}

func (s *Store) ModelRouteSettings(ctx context.Context, modelID string) (ModelRouteSettings, bool, error) {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	settings := ModelRouteSettings{ModelID: modelID, UsePlatformDefault: true}
	rows, err := s.db.QueryContext(ctx, `SELECT r.provider_id,p.name,p.base_url,p.official,r.enabled,r.priority,r.weight,r.input_per_million,r.output_per_million
FROM model_routes r JOIN provider_connections p ON p.id=r.provider_id
WHERE r.model_id=? ORDER BY r.priority,r.provider_id`, modelID)
	if err != nil {
		return settings, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var endpoint ModelRouteEndpoint
		var official, enabled int
		if err := rows.Scan(&endpoint.ProviderID, &endpoint.Name, &endpoint.BaseURL, &official, &enabled, &endpoint.Priority, &endpoint.Weight, &endpoint.InputPrice, &endpoint.OutputPrice); err != nil {
			return settings, false, err
		}
		endpoint.Official, endpoint.Enabled = official != 0, enabled != 0
		settings.Endpoints = append(settings.Endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return settings, false, err
	}
	if len(settings.Endpoints) == 0 {
		return settings, false, nil
	}
	var usePlatformDefault int
	err = s.db.QueryRowContext(ctx, `SELECT use_platform_default FROM model_route_preferences WHERE model_id=?`, modelID).Scan(&usePlatformDefault)
	if err != nil && err != sql.ErrNoRows {
		return settings, false, err
	}
	settings.UsePlatformDefault = err == sql.ErrNoRows || usePlatformDefault != 0
	return settings, true, nil
}

func (s *Store) SaveModelRouteSettings(ctx context.Context, settings ModelRouteSettings) error {
	settings.ModelID = strings.ToLower(strings.TrimSpace(settings.ModelID))
	if settings.ModelID == "" || len(settings.Endpoints) == 0 {
		return fmt.Errorf("model route settings require at least one endpoint")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expected int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_routes WHERE model_id=?`, settings.ModelID).Scan(&expected); err != nil {
		return err
	}
	if expected == 0 || expected != len(settings.Endpoints) {
		return fmt.Errorf("route endpoint list is incomplete")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make(map[string]struct{}, len(settings.Endpoints))
	for index, endpoint := range settings.Endpoints {
		endpoint.ProviderID = strings.ToLower(strings.TrimSpace(endpoint.ProviderID))
		if endpoint.ProviderID == "" {
			return fmt.Errorf("provider ID is required")
		}
		if _, exists := seen[endpoint.ProviderID]; exists {
			return fmt.Errorf("provider %q appears more than once", endpoint.ProviderID)
		}
		seen[endpoint.ProviderID] = struct{}{}
		priority, weight := (index+1)*10, endpoint.Weight
		if settings.UsePlatformDefault {
			priority, weight = 100, 100
		}
		if weight < 1 || weight > 10000 {
			return fmt.Errorf("route weight must be between 1 and 10000")
		}
		result, err := tx.ExecContext(ctx, `UPDATE model_routes SET enabled=?,priority=?,weight=?,updated_at=? WHERE model_id=? AND provider_id=?`,
			boolInt(endpoint.Enabled), priority, weight, now, settings.ModelID, endpoint.ProviderID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return fmt.Errorf("provider %q is not an endpoint for model %q", endpoint.ProviderID, settings.ModelID)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO model_route_preferences(model_id,use_platform_default,created_at,updated_at) VALUES(?,?,?,?)
ON CONFLICT(model_id) DO UPDATE SET use_platform_default=excluded.use_platform_default,updated_at=excluded.updated_at`, settings.ModelID, boolInt(settings.UsePlatformDefault), now, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveConnection(ctx context.Context, update ConnectionUpdate) error {
	p := update.Provider
	if s.protector == nil && update.APIKey != nil && strings.TrimSpace(*update.APIKey) != "" {
		return fmt.Errorf("master key is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := strings.TrimSpace(update.Name)
	if name == "" {
		name = providerDisplayName(p.ID)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_connections
(id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,api_key_header,official,model_category,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,base_url=excluded.base_url,
chat_path=excluded.chat_path,responses_path=excluded.responses_path,embeddings_path=excluded.embeddings_path,
authentication=excluded.authentication,api_key_header=excluded.api_key_header,model_category=excluded.model_category,enabled=excluded.enabled,updated_at=excluded.updated_at`,
		p.ID, name, p.Type, p.BaseURL, p.ChatPath, p.ResponsesPath, p.EmbeddingsPath, p.Authentication, p.APIKeyHeader,
		boolInt(update.Official), update.ModelCategory, boolInt(update.Enabled), now, now)
	if err != nil {
		return err
	}
	if update.APIKey != nil {
		key := strings.TrimSpace(*update.APIKey)
		if key == "" {
			_, err = tx.ExecContext(ctx, `UPDATE provider_connections SET api_key_ciphertext=NULL,api_key_nonce=NULL,
validation_status='unverified',last_validated_at=NULL,last_error='',updated_at=? WHERE id=?`, now, p.ID)
		} else {
			ciphertext, nonce, cryptErr := s.protector.Encrypt(key, []byte("provider:"+p.ID))
			if cryptErr != nil {
				return cryptErr
			}
			_, err = tx.ExecContext(ctx, `UPDATE provider_connections SET api_key_ciphertext=?,api_key_nonce=?,
validation_status='unverified',last_validated_at=NULL,last_error='',updated_at=? WHERE id=?`, ciphertext, nonce, now, p.ID)
		}
		if err != nil {
			return err
		}
	}
	for _, m := range update.Models {
		m.Provider = p.ID
		if err := insertModel(ctx, tx, m, false, false, now); err != nil {
			return err
		}
	}
	for _, policy := range update.Routes {
		result, err := tx.ExecContext(ctx, `UPDATE model_routes SET priority=?,weight=?,updated_at=? WHERE model_id=? AND provider_id=?`,
			policy.Priority, policy.Weight, now, strings.ToLower(strings.TrimSpace(policy.ModelID)), p.ID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return fmt.Errorf("model route %q was not found for provider %q", policy.ModelID, p.ID)
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// model_configs predates multi-route support and retains a canonical provider
	// reference. Move that reference before deleting a route so other routes for
	// the same public model are not removed by the legacy cascade.
	if _, err := tx.ExecContext(ctx, `UPDATE model_configs SET provider_id=(
SELECT r.provider_id FROM model_routes r WHERE r.model_id=model_configs.id AND r.provider_id<>? ORDER BY r.priority,r.provider_id LIMIT 1)
WHERE provider_id=? AND EXISTS (SELECT 1 FROM model_routes r WHERE r.model_id=model_configs.id AND r.provider_id<>?)`, id, id, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM provider_connections WHERE id=? AND official=0`, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return fmt.Errorf("custom provider not found")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM model_configs WHERE NOT EXISTS (SELECT 1 FROM model_routes r WHERE r.model_id=model_configs.id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RoutingRules(ctx context.Context) ([]catalog.RoutingRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.name,r.enabled,m.model_id,m.priority,m.weight
FROM routing_rules r LEFT JOIN routing_rule_members m ON m.rule_id=r.id
ORDER BY r.name,r.id,m.priority,m.model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]catalog.RoutingRule, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		var id, name string
		var enabled int
		var modelID sql.NullString
		var priority, weight sql.NullInt64
		if err := rows.Scan(&id, &name, &enabled, &modelID, &priority, &weight); err != nil {
			return nil, err
		}
		index, ok := indexes[id]
		if !ok {
			index = len(rules)
			indexes[id] = index
			rules = append(rules, catalog.RoutingRule{ID: id, Name: name, Enabled: enabled == 1, Members: []catalog.RoutingRuleMember{}})
		}
		if modelID.Valid {
			rules[index].Members = append(rules[index].Members, catalog.RoutingRuleMember{ModelID: modelID.String, Priority: int(priority.Int64), Weight: int(weight.Int64)})
		}
	}
	return rules, rows.Err()
}

func (s *Store) ModelRoutePolicies(ctx context.Context) ([]ModelRoutePolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id,strategy,created_at,updated_at FROM model_route_policies ORDER BY model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]ModelRoutePolicy, 0)
	for rows.Next() {
		var policy ModelRoutePolicy
		var created, updated string
		if err := rows.Scan(&policy.ModelID, &policy.Strategy, &created, &updated); err != nil {
			return nil, err
		}
		policy.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		policy.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (s *Store) SaveModelRoutePolicy(ctx context.Context, policy ModelRoutePolicy) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at) VALUES(?,?,?,?)
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at`,
		strings.ToLower(strings.TrimSpace(policy.ModelID)), policy.Strategy, now, now)
	return err
}

func (s *Store) DeleteModelRoutePolicy(ctx context.Context, modelID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM model_route_policies WHERE model_id=?`, strings.ToLower(strings.TrimSpace(modelID)))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return fmt.Errorf("model route policy not found")
	}
	return nil
}

func (s *Store) SaveRoutingRule(ctx context.Context, rule catalog.RoutingRule) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO routing_rules(id,name,enabled,created_at,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,updated_at=excluded.updated_at`,
		rule.ID, rule.Name, boolInt(rule.Enabled), now, now)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM routing_rule_members WHERE rule_id=?`, rule.ID); err != nil {
		return err
	}
	for _, member := range rule.Members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO routing_rule_members(rule_id,model_id,priority,weight,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			rule.ID, member.ModelID, member.Priority, member.Weight, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteRoutingRule(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM routing_rules WHERE id=?`, strings.ToLower(strings.TrimSpace(id)))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return fmt.Errorf("routing rule not found")
	}
	return nil
}

func (s *Store) SetProviderValidation(ctx context.Context, id, status, message string) error {
	if status != "valid" && status != "invalid" && status != "temporarily_unavailable" && status != "unverified" {
		return fmt.Errorf("invalid provider validation status")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE provider_connections SET validation_status=?,last_validated_at=?,last_error=?,updated_at=? WHERE id=?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), message, time.Now().UTC().Format(time.RFC3339Nano), strings.ToLower(strings.TrimSpace(id)))
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func providerDisplayName(id string) string {
	names := map[string]string{"deepseek": "DeepSeek", "xiaomi": "Xiaomi MiMo", "tencent": "Tencent Hy", "openai": "OpenAI", "nvidia": "NVIDIA NIM", "zai": "Z.AI", "anthropic": "Anthropic", "minimax": "MiniMax", "qwen": "Alibaba Qwen", "gemini": "Google Gemini", "meta-llama": "Meta Llama", "meta-model-api": "Meta Model API", "mistral": "Mistral AI", "moonshot": "Moonshot AI", "poolside": "Poolside", "stepfun": "StepFun"}
	if name := names[id]; name != "" {
		return name
	}
	return id
}

func (s *Store) Record(ctx context.Context, u Usage) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO usage_events
(request_id,key_id,model,provider,endpoint,status,prompt_tokens,completion_tokens,cost_usd,latency_ms,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`, u.RequestID, u.KeyID, u.Model, u.Provider, u.Endpoint, u.Status,
		u.PromptTokens, u.CompletionTokens, u.CostUSD, u.LatencyMS, u.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) Recent(ctx context.Context, limit int) ([]Usage, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_id,key_id,model,provider,endpoint,status,prompt_tokens,
completion_tokens,cost_usd,latency_ms,created_at FROM usage_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Usage, 0)
	for rows.Next() {
		var u Usage
		var created string
		if err := rows.Scan(&u.RequestID, &u.KeyID, &u.Model, &u.Provider, &u.Endpoint, &u.Status,
			&u.PromptTokens, &u.CompletionTokens, &u.CostUSD, &u.LatencyMS, &created); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var out Summary
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(prompt_tokens),0),
COALESCE(SUM(completion_tokens),0),COALESCE(SUM(cost_usd),0),COALESCE(AVG(latency_ms),0) FROM usage_events`).
		Scan(&out.Requests, &out.PromptTokens, &out.CompletionTokens, &out.CostUSD, &out.AverageLatencyMS)
	return out, err
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }

func (u Usage) String() string {
	return fmt.Sprintf("%s %s %d", u.Provider, u.Model, u.Status)
}

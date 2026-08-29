#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
DATABASE_PATH=${OPENSWIFTSCALE_DATABASE_PATH:-$ROOT_DIR/data/openswiftscale.db}

command -v sqlite3 >/dev/null 2>&1 || {
  echo "sqlite3 is required to seed Qwen demo endpoints." >&2
  exit 1
}

if [ ! -f "$DATABASE_PATH" ]; then
  echo "OpenSwiftScale database not found: $DATABASE_PATH" >&2
  echo "Start the development gateway once before seeding examples." >&2
  exit 1
fi

case "${1:-seed}" in
  remove)
    sqlite3 "$DATABASE_PATH" <<'SQL'
BEGIN;
CREATE TABLE IF NOT EXISTS model_route_preferences (
  model_id TEXT PRIMARY KEY REFERENCES model_configs(id) ON DELETE CASCADE,
  use_platform_default INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
DELETE FROM provider_connections WHERE id IN ('demo-qwen-sg','demo-qwen-us','demo-qwen-tokyo');
UPDATE model_routes SET priority=100,weight=100,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE provider_id='qwen' AND model_id IN ('qwen3.8-max','qwen3.7-plus','qwen3.7-flash');
DELETE FROM model_route_policies WHERE model_id IN ('qwen3.8-max','qwen3.7-plus','qwen3.7-flash');
DELETE FROM model_route_preferences WHERE model_id IN ('qwen3.8-max','qwen3.7-plus','qwen3.7-flash');
COMMIT;
SQL
    echo "Removed Qwen demo endpoints and restored official route defaults."
    exit 0
    ;;
  seed) ;;
  *) echo "Usage: $0 [seed|remove]" >&2; exit 2 ;;
esac

sqlite3 "$DATABASE_PATH" <<'SQL'
BEGIN;

CREATE TABLE IF NOT EXISTS model_route_preferences (
  model_id TEXT PRIMARY KEY REFERENCES model_configs(id) ON DELETE CASCADE,
  use_platform_default INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT INTO provider_connections
(id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,api_key_header,region,official,model_category,enabled,validation_status,created_at,updated_at)
VALUES
('demo-qwen-sg','Demo ModelHub Singapore','openai-compatible','https://qwen-sg.demo.invalid/v1','/chat/completions','/responses','/embeddings','bearer','Authorization','apac',0,'open-source',1,'unverified',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('demo-qwen-us','Demo Inference US','openai-compatible','https://qwen-us.demo.invalid/openai/v1','/chat/completions','/responses','/embeddings','bearer','Authorization','us',0,'open-source',1,'unverified',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('demo-qwen-tokyo','Demo GPU Backup Tokyo','openai-compatible','https://qwen-tokyo.demo.invalid/v1','/chat/completions','/responses','/embeddings','bearer','Authorization','apac',0,'open-source',1,'unverified',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,base_url=excluded.base_url,
chat_path=excluded.chat_path,responses_path=excluded.responses_path,embeddings_path=excluded.embeddings_path,
authentication=excluded.authentication,api_key_header=excluded.api_key_header,model_category=excluded.model_category,
region=excluded.region,enabled=excluded.enabled,updated_at=excluded.updated_at;

UPDATE model_routes SET priority=10,weight=70,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE model_id='qwen3.8-max' AND provider_id='qwen';
UPDATE model_routes SET priority=10,weight=60,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE model_id='qwen3.7-plus' AND provider_id='qwen';
UPDATE model_routes SET priority=10,weight=50,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE model_id='qwen3.7-flash' AND provider_id='qwen';

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-sg','qwen/qwen3.8-max',capabilities_json,context_window,max_output_tokens,0.65,2.60,'USD',20,30,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.8-max'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;
INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-us','qwen-3.8-max',capabilities_json,context_window,max_output_tokens,0.58,2.40,'USD',30,1,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.8-max'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-sg','qwen/qwen3.7-plus',capabilities_json,context_window,max_output_tokens,0.42,1.65,'USD',20,40,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.7-plus'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;
INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-tokyo','qwen3.7-plus-turbo',capabilities_json,context_window,max_output_tokens,0.48,1.80,'USD',30,1,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.7-plus'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-sg','qwen/qwen3.7-flash',capabilities_json,context_window,max_output_tokens,0.08,0.32,'USD',20,30,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.7-flash'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;
INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-qwen-us','qwen-3.7-flash',capabilities_json,context_window,max_output_tokens,0.07,0.28,'USD',30,20,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM model_configs WHERE id='qwen3.7-flash'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=excluded.priority,weight=excluded.weight,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,CASE id WHEN 'qwen3.7-flash' THEN 'load-balance' ELSE 'hybrid' END,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id IN ('qwen3.8-max','qwen3.7-plus','qwen3.7-flash')
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;

INSERT INTO model_route_preferences(model_id,use_platform_default,created_at,updated_at)
SELECT id,0,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id IN ('qwen3.8-max','qwen3.7-plus','qwen3.7-flash')
ON CONFLICT(model_id) DO UPDATE SET use_platform_default=excluded.use_platform_default,updated_at=excluded.updated_at;

COMMIT;
SQL

echo "Seeded Qwen demo endpoints (reserved .invalid URLs; no credentials):"
sqlite3 "$DATABASE_PATH" "SELECT '  - ' || r.model_id || ' / ' || r.provider_id || ' / priority ' || r.priority || ' / weight ' || r.weight FROM model_routes r WHERE r.model_id LIKE 'qwen%' ORDER BY r.model_id,r.priority,r.provider_id;"
echo "Restart the development gateway so its in-memory routing catalog reloads these rows."

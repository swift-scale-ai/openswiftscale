#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
DATABASE_PATH=${OPENSWIFTSCALE_DATABASE_PATH:-$ROOT_DIR/data/openswiftscale.db}

command -v sqlite3 >/dev/null 2>&1 || {
  echo "sqlite3 is required to seed Alibaba media demo endpoints." >&2
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
DELETE FROM model_configs WHERE id IN ('happyhorse-video-demo','qwen-image-demo','wan-video-demo');
DELETE FROM provider_connections WHERE id IN ('demo-media-sg','demo-media-eu');
COMMIT;
SQL
    echo "Removed Alibaba media demo models and endpoints."
    exit 0
    ;;
  seed) ;;
  *) echo "Usage: $0 [seed|remove]" >&2; exit 2 ;;
esac

sqlite3 "$DATABASE_PATH" <<'SQL'
BEGIN;

INSERT INTO provider_connections
(id,name,type,base_url,chat_path,responses_path,embeddings_path,authentication,api_key_header,region,official,model_category,enabled,validation_status,created_at,updated_at)
VALUES
('demo-media-sg','Demo Media Cloud Singapore','openai-compatible','https://media-sg.demo.invalid/v1','/chat/completions','/responses','/embeddings','bearer','Authorization','apac',0,'commercial',1,'unverified',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('demo-media-eu','Demo Creative Inference EU','openai-compatible','https://media-eu.demo.invalid/openai/v1','/chat/completions','/responses','/embeddings','bearer','Authorization','europe',0,'commercial',1,'unverified',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,base_url=excluded.base_url,
chat_path=excluded.chat_path,responses_path=excluded.responses_path,embeddings_path=excluded.embeddings_path,
authentication=excluded.authentication,api_key_header=excluded.api_key_header,model_category=excluded.model_category,
region=excluded.region,enabled=excluded.enabled,updated_at=excluded.updated_at;

INSERT INTO model_configs
(id,name,family,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,fallbacks_json,metadata_json,enabled,created_at,updated_at)
VALUES
('happyhorse-video-demo','HappyHorse Video Demo','happyhorse','qwen','happyhorse-video-demo','["video"]',0,0,1.20,4.80,'USD','[]','{"demo":true}',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('qwen-image-demo','Qwen Image Demo','qwen-image','qwen','qwen-image-demo','["image"]',0,0,0.80,3.20,'USD','[]','{"demo":true}',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('wan-video-demo','Wan Video Demo','wan','qwen','wan-video-demo','["image","video"]',0,0,1.50,6.00,'USD','[]','{"demo":true}',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
ON CONFLICT(id) DO UPDATE SET name=excluded.name,family=excluded.family,provider_id=excluded.provider_id,
upstream_model=excluded.upstream_model,capabilities_json=excluded.capabilities_json,input_per_million=excluded.input_per_million,
output_per_million=excluded.output_per_million,currency=excluded.currency,metadata_json=excluded.metadata_json,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'qwen',upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,10,70,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id IN ('happyhorse-video-demo','qwen-image-demo','wan-video-demo')
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,capabilities_json=excluded.capabilities_json,
input_per_million=excluded.input_per_million,output_per_million=excluded.output_per_million,priority=10,weight=70,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-media-sg','demo/happyhorse-video',capabilities_json,context_window,max_output_tokens,0.95,4.20,'USD',20,30,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='happyhorse-video-demo'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,
output_per_million=excluded.output_per_million,priority=20,weight=30,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-media-eu','demo/qwen-image',capabilities_json,context_window,max_output_tokens,0.70,2.90,'USD',20,30,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='qwen-image-demo'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,
output_per_million=excluded.output_per_million,priority=20,weight=30,enabled=1,updated_at=excluded.updated_at;

INSERT INTO model_routes
(model_id,provider_id,upstream_model,capabilities_json,context_window,max_output_tokens,input_per_million,output_per_million,currency,priority,weight,enabled,created_at,updated_at)
SELECT id,'demo-media-sg','demo/wan-video',capabilities_json,context_window,max_output_tokens,1.25,5.40,'USD',20,30,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='wan-video-demo'
ON CONFLICT(model_id,provider_id) DO UPDATE SET upstream_model=excluded.upstream_model,input_per_million=excluded.input_per_million,
output_per_million=excluded.output_per_million,priority=20,weight=30,enabled=1,updated_at=excluded.updated_at;

COMMIT;
SQL

echo "Seeded Alibaba media demo models and endpoints (reserved .invalid URLs; no credentials):"
sqlite3 "$DATABASE_PATH" "SELECT '  - ' || r.model_id || ' / ' || r.provider_id || ' / priority ' || r.priority || ' / weight ' || r.weight FROM model_routes r WHERE r.model_id IN ('happyhorse-video-demo','qwen-image-demo','wan-video-demo') ORDER BY r.model_id,r.priority,r.provider_id;"
echo "Restart the development gateway so its in-memory routing catalog reloads these rows."

#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
DATABASE_PATH=${OPENSWIFTSCALE_DATABASE_PATH:-$ROOT_DIR/data/openswiftscale.db}

command -v sqlite3 >/dev/null 2>&1 || {
  echo "sqlite3 is required to seed local example routes." >&2
  exit 1
}

if [ ! -f "$DATABASE_PATH" ]; then
  echo "OpenSwiftScale database not found: $DATABASE_PATH" >&2
  echo "Start the development gateway once before seeding examples." >&2
  exit 1
fi

sqlite3 "$DATABASE_PATH" <<'SQL'
BEGIN;
CREATE TABLE IF NOT EXISTS model_route_policies (
  model_id TEXT PRIMARY KEY REFERENCES model_configs(id) ON DELETE CASCADE,
  strategy TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,'failover',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='deepseek-v4-flash'
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,'failover',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='claude-sonnet-5'
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,'failover',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='gpt-5.6-sol'
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,'failover',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='qwen3.8-max'
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;

INSERT INTO model_route_policies(model_id,strategy,created_at,updated_at)
SELECT id,'failover',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM model_configs WHERE id='gemini-3.1-pro-preview'
ON CONFLICT(model_id) DO UPDATE SET strategy=excluded.strategy,updated_at=excluded.updated_at;
COMMIT;
SQL

echo "Seeded example route rules:"
sqlite3 "$DATABASE_PATH" "SELECT '  - ' || p.model_id || ' (' || p.strategy || ', ' || COUNT(r.provider_id) || ' endpoint' || CASE WHEN COUNT(r.provider_id)=1 THEN '' ELSE 's' END || ')' FROM model_route_policies p LEFT JOIN model_routes r ON r.model_id=p.model_id GROUP BY p.model_id,p.strategy ORDER BY p.model_id;"

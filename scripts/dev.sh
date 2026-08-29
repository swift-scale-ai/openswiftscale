#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
FRONTEND_DIR="$ROOT_DIR/frontend"

detect_host_ip() {
  if command -v route >/dev/null 2>&1 && command -v ipconfig >/dev/null 2>&1; then
    interface=$(route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}')
    if [ -n "$interface" ]; then
      address=$(ipconfig getifaddr "$interface" 2>/dev/null || true)
      if [ -n "$address" ]; then
        printf '%s\n' "$address"
        return
      fi
    fi
  fi
  if command -v ifconfig >/dev/null 2>&1; then
    address=$(ifconfig 2>/dev/null | awk '/^[[:alnum:]]/{iface=$1; sub(":$", "", iface)} /inet / && $2 !~ /^127\./ {print $2; exit}')
    if [ -n "$address" ]; then
      printf '%s\n' "$address"
      return
    fi
  fi
  if command -v hostname >/dev/null 2>&1; then
    hostname -I 2>/dev/null | awk '{print $1}' || true
  fi
}

command -v go >/dev/null 2>&1 || { echo "Go is required for local development." >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "Node.js and npm are required for local development." >&2; exit 1; }

HOST_IP=$(detect_host_ip)
[ -n "$HOST_IP" ] || HOST_IP=127.0.0.1
export OPENSWIFTSCALE_LISTEN_ADDR=${OPENSWIFTSCALE_LISTEN_ADDR:-127.0.0.1:8080}
if [ "$HOST_IP" != "127.0.0.1" ]; then
  export OPENSWIFTSCALE_LAN_LISTEN_ADDR=${OPENSWIFTSCALE_LAN_LISTEN_ADDR:-$HOST_IP:8080}
fi
export OPENSWIFTSCALE_PUBLIC_URL=${OPENSWIFTSCALE_PUBLIC_URL:-http://$HOST_IP:8080}
export VITE_PUBLIC_GATEWAY_URL=${VITE_PUBLIC_GATEWAY_URL:-$OPENSWIFTSCALE_PUBLIC_URL}
export OPENSWIFTSCALE_DATABASE_PATH=${OPENSWIFTSCALE_DATABASE_PATH:-$ROOT_DIR/data/openswiftscale.db}
export OPENSWIFTSCALE_MASTER_KEY_PATH=${OPENSWIFTSCALE_MASTER_KEY_PATH:-$ROOT_DIR/data/keys/master.key}
export OPENSWIFTSCALE_CATALOG_PATH=${OPENSWIFTSCALE_CATALOG_PATH:-$ROOT_DIR/config/catalog.yaml}
export OPENSWIFTSCALE_API_KEYS=${OPENSWIFTSCALE_API_KEYS:-local-development-key}
export OPENSWIFTSCALE_ADMIN_USERNAME=${OPENSWIFTSCALE_ADMIN_USERNAME:-admin}
export OPENSWIFTSCALE_ADMIN_PASSWORD=${OPENSWIFTSCALE_ADMIN_PASSWORD:-openswiftscale}
case "$OPENSWIFTSCALE_LISTEN_ADDR" in
  :*) BACKEND_URL="http://127.0.0.1$OPENSWIFTSCALE_LISTEN_ADDR" ;;
  0.0.0.0:*) BACKEND_URL="http://127.0.0.1:${OPENSWIFTSCALE_LISTEN_ADDR##*:}" ;;
  *) BACKEND_URL="http://$OPENSWIFTSCALE_LISTEN_ADDR" ;;
esac
export VITE_BACKEND_URL=${VITE_BACKEND_URL:-$BACKEND_URL}

if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
  echo "Installing frontend dependencies..."
  (cd "$FRONTEND_DIR" && npm ci --no-audit --no-fund)
fi

if command -v curl >/dev/null 2>&1 && curl -fsS "$BACKEND_URL/healthz" >/dev/null 2>&1; then
  STATUS_PAYLOAD=$(curl -fsS -u "$OPENSWIFTSCALE_ADMIN_USERNAME:$OPENSWIFTSCALE_ADMIN_PASSWORD" "$BACKEND_URL/api/admin/status" 2>/dev/null || true)
  case "$STATUS_PAYLOAD" in
    *'"name":"OpenSwiftScale"'*)
      echo "Cannot start development backend: $BACKEND_URL is already serving OpenSwiftScale." >&2
      echo "Stop the existing OpenSwiftScale process or choose another address." >&2
      ;;
    *)
      echo "Cannot start development backend: $BACKEND_URL is occupied by another HTTP service." >&2
      echo "Use OPENSWIFTSCALE_LISTEN_ADDR=127.0.0.1:8081 ./scripts/dev.sh or stop that service." >&2
      ;;
  esac
  exit 1
fi

mkdir -p "$ROOT_DIR/data"
BACKEND_PID=""

cleanup() {
  if [ -n "$BACKEND_PID" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT HUP INT TERM

echo "Starting Go gateway at $BACKEND_URL..."
echo "Internal gateway API: $OPENSWIFTSCALE_PUBLIC_URL/v1"
(cd "$ROOT_DIR" && go run ./cmd/openswiftscale) &
BACKEND_PID=$!

echo "Starting Vite console at http://127.0.0.1:5173..."
echo "Administrator username: $OPENSWIFTSCALE_ADMIN_USERNAME"
echo "Administrator password: $OPENSWIFTSCALE_ADMIN_PASSWORD"
cd "$FRONTEND_DIR"
npm run dev -- --host 127.0.0.1

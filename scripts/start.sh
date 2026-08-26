#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/deploy/compose.yaml"
GATEWAY_KEY_FILE="$ROOT_DIR/secrets/gateway_api_keys.txt"
ADMIN_PASSWORD_FILE="$ROOT_DIR/secrets/admin_password.txt"

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

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required. Install Docker Desktop or Docker Engine first." >&2
  exit 1
}
docker compose version >/dev/null 2>&1 || {
  echo "Docker Compose v2 is required." >&2
  exit 1
}

if [ ! -s "$GATEWAY_KEY_FILE" ] || [ ! -s "$ADMIN_PASSWORD_FILE" ]; then
  echo "First start: creating local OpenSwiftScale credentials..."
  "$SCRIPT_DIR/install.sh"
fi

HOST_IP=$(detect_host_ip)
[ -n "$HOST_IP" ] || HOST_IP=127.0.0.1
export OPENSWIFTSCALE_BIND_ADDRESS=${OPENSWIFTSCALE_BIND_ADDRESS:-$HOST_IP}
export OPENSWIFTSCALE_PUBLIC_URL=${OPENSWIFTSCALE_PUBLIC_URL:-http://$HOST_IP:8080}

echo "Starting OpenSwiftScale..."
docker compose -f "$COMPOSE_FILE" up -d --remove-orphans

attempt=0
while [ "$attempt" -lt 20 ]; do
  if docker compose -f "$COMPOSE_FILE" exec -T gateway /app/openswiftscale healthcheck >/dev/null 2>&1; then
    echo "OpenSwiftScale is ready."
    echo "Console: http://127.0.0.1:8080"
    echo "Internal gateway API: $OPENSWIFTSCALE_PUBLIC_URL/v1"
    echo "Sign in with the administrator account created by scripts/install.sh."
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done

echo "OpenSwiftScale was started but did not become healthy in time." >&2
echo "Check logs with: ./scripts/logs.sh" >&2
exit 1

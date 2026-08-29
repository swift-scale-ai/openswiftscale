#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
SECRETS_DIR="$ROOT_DIR/secrets"

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

command -v docker >/dev/null 2>&1 || { echo "Docker is required." >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "Docker Compose v2 is required." >&2; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo "OpenSSL is required to create local credentials." >&2; exit 1; }

mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

write_random_secret() {
  target="$1"
  if [ ! -s "$target" ]; then
    openssl rand -hex 32 > "$target"
    chmod 600 "$target"
  fi
}

write_random_secret "$SECRETS_DIR/gateway_api_keys.txt"
if [ ! -s "$SECRETS_DIR/admin_password.txt" ] && [ -s "$SECRETS_DIR/management_token.txt" ]; then
  cp "$SECRETS_DIR/management_token.txt" "$SECRETS_DIR/admin_password.txt"
  chmod 600 "$SECRETS_DIR/admin_password.txt"
fi
write_random_secret "$SECRETS_DIR/admin_password.txt"

HOST_IP=$(detect_host_ip)
[ -n "$HOST_IP" ] || HOST_IP=127.0.0.1

echo "OpenSwiftScale local credentials are ready."
echo "Start OpenSwiftScale with: $ROOT_DIR/scripts/start.sh"
echo "Console: http://127.0.0.1:8080"
echo "Internal gateway API: http://$HOST_IP:8080/v1"
echo "Gateway API key: $(sed -n '1p' "$SECRETS_DIR/gateway_api_keys.txt")"
echo "Administrator username: admin"
echo "Administrator password: $(sed -n '1p' "$SECRETS_DIR/admin_password.txt")"
echo "After signing in, open Models and configure a model endpoint with its provider API key."

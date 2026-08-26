# Operations guide

## Install and start

```bash
./scripts/install.sh
./scripts/start.sh
```

The installer validates Docker, creates bootstrap credentials, and prepares local configuration. The start script launches Docker Compose, waits for health, detects an active LAN address, and prints the console and internal Gateway API addresses.

Useful lifecycle commands:

```bash
./scripts/logs.sh
./scripts/stop.sh
./scripts/start.sh
```

Users do not need to invoke `docker compose` directly.

## First configuration

1. Sign in with the administrator account printed by the installer.
2. Open **Connections** and configure at least one official or third-party provider API key.
3. Open **Route rules** and create the desired model-ID endpoint policy.
4. Open **API access**, create an API user, and issue a Gateway API key.
5. Copy the API base URL and a callable model ID from **Overview**.
6. Test with one of the built-in cURL, Python, Go, Node.js, Java, or Rust examples.

## Networking

The inference and management service listens on port `8080`. `scripts/start.sh` publishes it on the detected host LAN address. Set these explicitly for private DNS, VPNs, reverse proxies, or internal load balancers:

```bash
export OPENSWIFTSCALE_BIND_ADDRESS=192.168.1.25
export OPENSWIFTSCALE_PUBLIC_URL=https://ai-gateway.internal.example.com
./scripts/start.sh
```

Terminate TLS at a trusted reverse proxy or ingress before allowing traffic across hosts. Restrict management access more tightly than inference access. Port `5173` is only used by Vite during source development.

## Health and observability

- `GET /healthz`: process health.
- `GET /readyz`: database readiness.
- `GET /metrics`: Prometheus-format operational metrics.
- **Overview**: request, latency, provider, token, and estimated-cost visualizations.
- **Usage**: recent request metadata without prompt bodies.

Estimated cost is derived from locally configured model prices and reported token counts. It is not a provider invoice.

## Upgrades

1. Read release notes and back up SQLite plus the master key.
2. Pull the desired image tag or source version.
3. Run `./scripts/start.sh` to recreate the container using the persistent volume.
4. Check `/readyz`, configured routes, callable models, and a real inference request.

Pin a version or image digest in production rather than relying on `latest`. Schema migrations run at startup; do not downgrade a production database without a tested restore plan.

## Failure recovery

- A missing provider key makes its routes unavailable but does not stop the gateway.
- A disabled API user blocks all keys owned by that user.
- A lost managed Gateway API key cannot be recovered; revoke it and create another.
- A lost master key makes encrypted provider credentials unrecoverable; restore the matching key or enter credentials again.
- If port `8080` is already serving OpenSwiftScale, stop that instance rather than starting a second gateway on a different port unintentionally.

See [Data and storage](DATA_AND_STORAGE.md) and [Security](../SECURITY.md) for backups and production hardening.


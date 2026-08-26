# Development guide

## Prerequisites

- Go 1.24 or newer.
- Node.js 22 and npm for the console build.
- Docker Engine and Docker Compose v2 for container validation.
- A supported macOS or Linux development environment.

## Start the development environment

```bash
./scripts/dev.sh
```

The script starts:

- Go gateway and management API at `http://127.0.0.1:8080`.
- Vite development console at `http://127.0.0.1:5173`.
- Hot module replacement for React and TypeScript changes.
- Vite proxies for `/api`, `/v1`, `/healthz`, `/readyz`, and `/metrics` to Go.

Port `5173` is only a frontend development server. Applications call the gateway on port `8080`; the console Overview page uses `OPENSWIFTSCALE_PUBLIC_URL` to show the LAN-reachable inference address.

The default development administrator is `admin` / `openswiftscale`. Development data is persisted in `data/openswiftscale.db` and remains after a restart.

`scripts/dev.sh` refuses to start a second backend when the configured address already serves OpenSwiftScale. Stop the existing process and reuse port `8080` rather than accidentally testing against two different databases.

## Build and test

```bash
npm --prefix frontend ci
npm --prefix frontend run build
gofmt -w cmd internal
go vet ./...
go test ./...
go build ./cmd/openswiftscale
```

The common backend checks are also available as:

```bash
make check
```

The frontend production build is written to `internal/webui/dist` and embedded into the Go binary. Node.js is not required at runtime.

## Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/openswiftscale` | Process entry point and healthcheck command. |
| `internal/gateway` | HTTP routing, authentication orchestration, management API, and inference proxy. |
| `internal/router` | Priority, weight, failover, and alias resolution. |
| `internal/provider` | Upstream HTTP and protocol adapters. |
| `internal/store` | SQLite schema, configuration, API users, keys, and usage records. |
| `internal/secret` | Master-key handling and provider-credential encryption. |
| `frontend` | Vite, React, and TypeScript management console. |
| `internal/webui/dist` | Generated embedded console assets. |
| `config/catalog.yaml` | Versioned provider/model seed catalog. |
| `scripts` | Install, development, lifecycle, build, and example helpers. |
| `deploy` | Docker Compose deployment definition. |

## Adding a provider or model

Use an official provider contract. Add seed metadata to `config/catalog.yaml`, keep the Public Model ID distinct from the upstream provider ID, specify only supported capabilities, and add catalog/router/provider tests. Prices are estimates and must be documented as such.

Third-party and local OpenAI-compatible endpoints normally do not require source changes; add them through **Console → Connections**.

## Pull requests

Keep changes focused, do not include generated databases or secrets, update English documentation for user-visible behavior, add translations for console strings, and include tests proportional to the request-path risk. See [Contributing](../CONTRIBUTING.md).


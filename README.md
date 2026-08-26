<div align="center">

# OpenSwiftScale

### Your models. Your keys. Your network.

A lightweight, open-source, self-hosted AI gateway powered by SwiftScale.

[![CI](https://github.com/swift-scale-ai/OpenSwiftScale/actions/workflows/ci.yml/badge.svg)](https://github.com/swift-scale-ai/OpenSwiftScale/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/swift-scale-ai/OpenSwiftScale)](https://github.com/swift-scale-ai/OpenSwiftScale/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[Quick start](#quick-start) · [Gateway API](docs/API.md) · [Documentation](#documentation) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

</div>

OpenSwiftScale gives developers and teams one OpenAI-compatible endpoint for directly connected model providers. It runs without a SwiftScale account, keeps provider keys inside your network, stores operational data locally, and sends no telemetry by default.

## Documentation

| Guide | Purpose |
| --- | --- |
| [Gateway API](docs/API.md) | Authentication, callable model IDs, SDK examples, errors, and routing headers. |
| [Configuration](docs/CONFIGURATION.md) | Runtime variables, connections, API users, model routes, and aliases. |
| [Data and storage](docs/DATA_AND_STORAGE.md) | SQLite contents, secret handling, backup, restore, reset, and retention. |
| [Operations](docs/OPERATIONS.md) | Install, start, stop, logs, upgrades, networking, and production checklist. |
| [Development](docs/DEVELOPMENT.md) | Source layout, local ports, toolchain, tests, and build workflow. |
| [Releases](docs/RELEASES.md) | Versioning, automated artifacts, container images, and verification. |
| [Architecture](docs/ARCHITECTURE.md) | Runtime components, request path, persistence model, and extension boundaries. |
| [Product design](docs/PRODUCT.md) | Product principles, Community scope, non-goals, and success criteria. |
| [Open-source policy](docs/OPEN_SOURCE.md) | License rights, Community guarantees, trademarks, contributions, and dependencies. |
| [Community and Enterprise](docs/ENTERPRISE.md) | Clear feature boundary between free Community, Enterprise, and Cloud. |
| [Security](SECURITY.md) | Vulnerability reporting and secure deployment baseline. |
| [Support](SUPPORT.md) | Community support scope and safe issue-reporting guidance. |
| [Code of Conduct](CODE_OF_CONDUCT.md) | Expected behavior in project spaces. |

## Why OpenSwiftScale

- **Local-first:** one container, embedded console, SQLite by default.
- **No model aggregator:** OpenSwiftScale connects directly to configured provider APIs.
- **OpenAI-compatible:** use existing OpenAI SDKs with a different base URL.
- **Observable:** local usage, estimated cost, latency, Prometheus metrics, and request IDs.
- **Resilient:** multiple routes per public model, priority policies, weighted traffic, and automatic failover.
- **Guided routing:** configure model-ID endpoint pools through a failover, load-balancing, or hybrid wizard.
- **Optional aliases:** advanced virtual model aliases can route across different public models.
- **Auditable:** Apache-2.0 source, reproducible releases, SBOMs, and provenance attestations.
- **Private by default:** prompt logging and telemetry are disabled.

## Quick start

Requirements: Docker Engine and Docker Compose v2.

```bash
git clone https://github.com/swift-scale-ai/OpenSwiftScale.git
cd OpenSwiftScale
./scripts/install.sh
./scripts/start.sh
```

`scripts/start.sh` checks Docker, creates missing first-run credentials, starts the container, waits for health, and prints the console address. It is safe to use the same command for later restarts. Users do not need to run Docker Compose commands directly.

The start script also detects the host's active LAN IP, publishes the gateway on that address, and prints the internal API URL. Override `OPENSWIFTSCALE_BIND_ADDRESS` and `OPENSWIFTSCALE_PUBLIC_URL` when deploying behind private DNS, a VPN, reverse proxy, or internal load balancer.

To follow gateway logs, run `./scripts/logs.sh`.

For local UI demonstrations, seed a small set of route rules backed only by endpoints already present in the local catalog:

```bash
./scripts/seed-example-routes.sh
```

The script does not create fake providers or duplicate endpoints. It can be run repeatedly.

Open the console at <http://127.0.0.1:8080>. Sign in with the administrator username and randomly generated password printed by the installer. Then open **Connections** and paste a provider API key into one of the prefilled official providers. Open **API access** to create users and issue separate, revocable Gateway API keys for applications and team members. Saving a provider connection does not contact the provider or block startup.

Custom OpenAI-compatible and Anthropic-compatible endpoints can be added from the same page. Provider credentials are encrypted with AES-256-GCM before being stored in SQLite; the master key is kept separately under `/data/keys/master.key` by default.

The same **Public model ID** may be used by multiple connections. Give preferred routes a lower priority number, and use weights to distribute new requests between routes with the same priority. If a selected route cannot connect or returns `401`, `403`, `408`, `429`, or `5xx`, OpenSwiftScale automatically tries the next eligible route.

The console's model-routing wizard guides administrators through selecting the exact Public Model ID, choosing failover, load-balancing, or hybrid behavior, configuring endpoints, and reviewing the final request path. Advanced virtual model aliases add an optional second level: an alias contains different Public Model IDs as members and supports chains such as DeepSeek → GPT → Claude without changing client configuration.

Send a request:

```bash
export OPEN_SWIFT_SCALE_KEY="<key-created-in-api-access>"

curl http://192.168.1.25:8080/v1/chat/completions \
  -H "Authorization: Bearer $OPEN_SWIFT_SCALE_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.6-luna",
    "messages": [{"role": "user", "content": "Explain this architecture."}],
    "stream": false
  }'
```

See the [Gateway API guide](docs/API.md) for authentication, model discovery, SDK examples, routing behavior, response headers, and production network guidance.

For local source development:

```bash
./scripts/dev.sh
```

The development script starts both services: the Go gateway on `127.0.0.1:8080` and the Vite + React + TypeScript console on `127.0.0.1:5173`. Open the Vite address during development for automatic browser updates. API, health, readiness, and metrics requests are proxied to Go.

Port `5173` is only the development UI and is not an inference endpoint. The Overview page advertises the gateway's configured LAN or private-DNS URL on port `8080` (unless explicitly overridden).

If another local application already uses port 8080, choose a different backend address; Vite automatically proxies to it:

```bash
OPENSWIFTSCALE_LISTEN_ADDR=127.0.0.1:8081 ./scripts/dev.sh
```

The default development console account is `admin` with password `openswiftscale`. Production installation generates a unique random administrator password instead.

Production builds remain a single self-contained Go executable. Vite writes its optimized output to `internal/webui/dist`, and Go embeds that directory at compile time:

```bash
./scripts/build.sh
```

## API surface

| Method | Endpoint | Status |
| --- | --- | --- |
| `GET` | `/v1/models` | Implemented |
| `POST` | `/v1/chat/completions` | Implemented, including SSE |
| `POST` | `/v1/responses` | Implemented for compatible providers |
| `POST` | `/v1/embeddings` | Implemented for catalog entries with embedding capability |
| `GET` | `/healthz` | Implemented |
| `GET` | `/readyz` | Implemented |
| `GET` | `/metrics` | Implemented |
| `GET` | `/api/admin/providers` | Provider connections and masked key state |
| `POST` | `/api/admin/providers` | Save an official or custom connection without startup validation |
| `DELETE` | `/api/admin/providers/{id}` | Delete a custom connection |
| `GET` | `/api/admin/model-route-policies` | List explicitly configured model route rules |
| `POST` | `/api/admin/model-route-policies` | Save a model route strategy |
| `DELETE` | `/api/admin/model-route-policies/{id}` | Remove a model route rule without deleting endpoints |
| `GET` | `/api/admin/routing-rules` | List advanced virtual model aliases |
| `POST` | `/api/admin/routing-rules` | Create or update a virtual model alias |
| `DELETE` | `/api/admin/routing-rules/{id}` | Delete a virtual model alias |
| `GET` | `/api/admin/users` | List API users and their masked key metadata |
| `POST` | `/api/admin/users` | Create an API user |
| `PATCH` | `/api/admin/users/{id}` | Update or disable an API user |
| `DELETE` | `/api/admin/users/{id}` | Delete a user and revoke all owned keys |
| `POST` | `/api/admin/users/{id}/keys` | Issue a key; the plaintext is returned once |
| `DELETE` | `/api/admin/users/{id}/keys/{key_id}` | Revoke an API key |

The embedded console uses separate management authentication and exposes provider setup, custom endpoint creation, API users and keys, gateway status, model readiness, request history, tokens, estimated cost, and latency.

## Initial model catalog

The seed catalog currently covers GPT, Claude, Gemini, DeepSeek, Qwen, GLM, MiniMax, Tencent HY, Xiaomi MiMo, and NVIDIA Nemotron families. It includes commercial and open-weight models while keeping official publisher endpoints separate from third-party or locally operated compatible endpoints.

OpenSwiftScale does **not** integrate with or depend on OpenRouter and does not route prompts through an aggregator. Provider model IDs, prices, context limits, and availability change independently; treat [`config/catalog.yaml`](config/catalog.yaml) as versioned seed data and verify each provider contract before production use.

## Local data

Local source development persists runtime state in `data/openswiftscale.db`. Docker deployments use the `openswiftscale_data` named volume and store the database at `/data/openswiftscale.db`. Provider credentials are encrypted before storage; managed Gateway API keys are stored only as SHA-256 hashes; prompt and response bodies are not persisted.

See [Data and storage](docs/DATA_AND_STORAGE.md) before backing up, resetting, or upgrading an installation.

## Community and enterprise

OpenSwiftScale Community Edition is free under Apache-2.0 and does not limit requests, tokens, providers, or local deployment. OpenSwiftScale Enterprise can add SSO, SCIM, advanced RBAC, fleet management, approval workflows, compliance policy packs, long-term audit retention, air-gapped lifecycle management, and commercial support without weakening the Community gateway.

See [Enterprise model](docs/ENTERPRISE.md).

## Project status

This repository currently contains the first functional Community MVP. It is suitable for local evaluation and contribution. Before production use, pin image digests, review provider-specific contracts, configure TLS at the ingress, test failure behavior, and establish database backups.

## License

Apache License 2.0. See [LICENSE](LICENSE).

The Apache license covers the Community source in this repository. “OpenSwiftScale”, “SwiftScale”, associated logos, and product branding remain trademarks; see [Open-source policy](docs/OPEN_SOURCE.md).

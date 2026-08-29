# Technical architecture

## Runtime view

```text
Application / OpenAI SDK
           |
           v
+---------------------------------------------+
| OpenSwiftScale                              |
|  API auth -> model resolver -> provider     |
|  protocol adapter -> stream forwarding      |
|  usage/cost recorder -> embedded console    |
+----------------------+----------------------+
                       |
       +---------------+----------------+
       |               |                |
       v               v                v
 OpenAI-compatible  Anthropic API   Local inference
 providers          translation     endpoints
```

## Process model

The default deployment is one Go process. It contains:

- Public OpenAI-compatible API.
- Management API protected by a distinct administrator account.
- API-user registry with independently revocable, hash-only Gateway API keys.
- SQLite-backed provider and model registry, initially seeded from YAML.
- Multi-route model router with priorities, weighted selection, and configured fallback chains.
- OpenAI-compatible proxy adapter.
- Anthropic Messages request/response translation.
- SQLite usage store in WAL mode.
- Embedded static web console.
- Health, readiness, request IDs, and Prometheus counters.

The console source is a Vite + React + TypeScript application under `frontend/`. Production builds compile it into `internal/webui/dist`, which is embedded in the Go executable. Node.js is a development and build dependency only; no Node.js runtime, Redis, PostgreSQL, queue, or SwiftScale control plane is required in deployment.

During local development, `scripts/dev.sh` runs the Go gateway on port 8080 and Vite on port 5173. Vite proxies API and operational endpoints to Go and provides hot module replacement. `scripts/build.sh`, Docker, CI, and release workflows build the console before compiling Go.

## Request path

1. Assign an `X-OpenSwiftScale-Request-ID`.
2. Authenticate either a bootstrap Gateway API key or a managed API-user key. Managed keys are matched by SHA-256 hash and disabled users are rejected.
3. Enforce the configured request body limit.
4. Parse the requested public model ID.
5. Resolve the requested exact model ID and validate endpoint capability.
6. Apply its explicit routing mode: platform scoring, strict sequential failover, or weighted distribution across participating endpoints.
7. On connection errors, timeouts, rejected credentials, throttling, or upstream server errors, advance only through endpoints serving that same exact model ID.
8. Rewrite the public model ID to the selected provider model ID.
9. Replace the client Authorization header with the provider credential.
10. Forward the request or translate it to Anthropic Messages.
11. Stream provider bytes/events without buffering the complete response.
12. Extract usage when the provider supplies it.
13. Persist request metadata, tokens, cost estimate, status, latency, and the provider that served the request to SQLite.

Prompt and response bodies are not persisted.

## Product boundary

OpenSwiftScale is technically independent from SwiftScale. It does not share backend services, databases, accounts, configuration, queues, internal packages, or runtime state with SwiftScale products.

OpenSwiftScale Cloud and the self-hosted edition share public contracts and product conventions rather than a required live backend dependency: brand, user experience, model hierarchy, exact-model routing invariant, and OpenAI-compatible API behavior. Cloud-only account, balance, payment, and managed-endpoint services stay outside the self-hosted process.

## Persistence

SQLite is the default because it keeps single-node deployment small and operationally understandable. WAL mode and a busy timeout are enabled. Prompt and response bodies are never persisted. Provider API keys are stored only as AES-256-GCM ciphertext with a unique nonce; the master encryption key is kept outside SQLite.

`api_users` owns application, person, team, or environment identities. `api_keys` stores key labels, safe prefixes, SHA-256 hashes, enabled state, creation time, and last-used time. Plaintext managed keys are returned once at creation and cannot be recovered from the database. Disabling a user invalidates all owned keys; deleting a user cascades to those keys.

Public model metadata and provider routes are stored separately. `model_configs` owns the stable public model identity; `model_routes` maps it to one or more provider connections and stores the upstream model ID, capability set, priority, weight, limits, local price estimate, success/failure counters, latency EWMA, and most recent health result. The `(public model ID, provider connection ID)` pair is unique, while a public model ID can appear in any number of routes.

`model_route_preferences` stores the per-model routing mode (`platform`, `failover`, or `weighted`) plus the optional regional preference. Participation, priority, and weight remain properties of each `model_routes` row. Failover mode persists distinct priorities and neutral weights; weighted mode persists one priority group and relative weights. Disabling a route does not delete its connection or prevent it from being enabled again.

The official YAML catalog is seed data rather than the runtime source of truth. On first start, official provider templates and models are inserted into SQLite. Saving a connection through the management API refreshes the in-memory routing snapshot atomically, so inference does not require a restart. Startup and readiness never depend on upstream API-key validation. Administrators can explicitly run a read-only `/v1/models` probe; real inference attempts also update route health observations and refresh the default score.

The default runtime remains intentionally single-node. PostgreSQL, Redis, and distributed coordination are not part of the self-hosted product unless a concrete developer use case justifies their operational cost.

## Extension boundaries

The initial implementation separates:

- `catalog`: provider and model contracts.
- `router`: exact-model capability filtering, priority ordering, weighted selection, and failover resolution.
- `provider`: HTTP and protocol translation.
- `auth`: bootstrap API-key and administrator-request parsing.
- `store`: provider/model configuration, managed API users and keys, routing policy, and operational usage records.
- `secret`: local master-key creation and authenticated credential encryption.
- `gateway`: HTTP orchestration.
- `webui`: embedded management console.

Go's runtime plugin mechanism is intentionally avoided because it complicates portability and supply-chain review.

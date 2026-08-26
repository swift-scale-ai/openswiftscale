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
- Advanced virtual model aliases with public Alias IDs and ordered/weighted model members.
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
5. Resolve the requested ID as either a Public Model ID or a cross-model Route ID and validate endpoint capability.
6. For an advanced Alias ID, order member models by priority and select among equal-priority models by relative weight.
7. For each selected model, order provider endpoints by route priority and select among equal-priority endpoints by relative weight.
8. On connection errors, timeouts, rejected credentials, throttling, or upstream server errors, advance through endpoints and then member models.
9. Rewrite the public model ID to the selected provider model ID.
10. Replace the client Authorization header with the provider credential.
11. Forward the request or translate it to Anthropic Messages.
12. Stream provider bytes/events without buffering the complete response.
13. Extract usage when the provider supplies it.
14. Persist request metadata, tokens, cost estimate, status, latency, and the provider that served the request to SQLite.

Prompt and response bodies are not persisted.

## Open-core boundary

The public Go module is the source of truth for the request path, provider contracts, local storage, and Community console. Commercial SwiftScale products should import this kernel and add private implementations around its interfaces rather than maintain a fork.

Enterprise extensions may provide centralized identity, SSO/SCIM, fleet configuration, audit export, policy approval, high-availability coordination, and commercial support. SwiftScale Cloud may provide managed inference, global routing, billing, and proprietary routing intelligence.

## Persistence

SQLite is the default because it keeps single-node deployment small and operationally understandable. WAL mode and a busy timeout are enabled. Prompt and response bodies are never persisted. Provider API keys are stored only as AES-256-GCM ciphertext with a unique nonce; the master encryption key is kept outside SQLite.

`api_users` owns application, person, team, or environment identities. `api_keys` stores key labels, safe prefixes, SHA-256 hashes, enabled state, creation time, and last-used time. Plaintext managed keys are returned once at creation and cannot be recovered from the database. Disabling a user invalidates all owned keys; deleting a user cascades to those keys.

Public model metadata and provider routes are stored separately. `model_configs` owns the stable public model identity; `model_routes` maps it to one or more provider connections and stores the upstream model ID, capability set, priority, weight, limits, and local price estimate. The `(public model ID, provider connection ID)` pair is unique, while a public model ID can appear in any number of routes.

`model_route_policies` records which model route groups an administrator explicitly created through the console and stores the selected failover, load-balancing, or hybrid strategy. Seeded catalog models are not automatically promoted to route rules.

`routing_rules` owns public Route IDs and `routing_rule_members` maps each rule to different Public Model IDs with a priority and weight. Rules cannot recursively contain other rules in the Community implementation, which keeps resolution bounded and inspectable.

The official YAML catalog is seed data rather than the runtime source of truth. On first start, official provider templates and models are inserted into SQLite. Saving a connection through the management API refreshes the in-memory routing snapshot atomically, so inference does not require a restart. Startup and readiness never depend on upstream API-key validation.

Future team and HA profiles may add PostgreSQL. Redis must remain optional and should only be introduced for cross-instance coordination that cannot be handled by PostgreSQL.

## Extension boundaries

The initial implementation separates:

- `catalog`: provider and model contracts.
- `router`: capability filtering, priority ordering, weighted selection, and failover resolution.
- `provider`: HTTP and protocol translation.
- `auth`: bootstrap API-key and administrator-request parsing.
- `store`: provider/model configuration, managed API users and keys, routing policy, and operational usage records.
- `secret`: local master-key creation and authenticated credential encryption.
- `gateway`: HTTP orchestration.
- `webui`: embedded management console.

Go's runtime plugin mechanism is intentionally avoided because it complicates portability and supply-chain review. Enterprise builds should use compile-time composition or a separately authenticated control-plane service.

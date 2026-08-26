# Configuration

## Required settings

| Variable | Purpose |
| --- | --- |
| `OPENSWIFTSCALE_API_KEYS` or `_FILE` | Comma-separated keys accepted by the inference API. |
| `OPENSWIFTSCALE_ADMIN_PASSWORD` or `_FILE` | Password for the administrator console and `/api/admin/*`. |

Provider credentials are not required at startup. Configure them later in the embedded console. Models only appear in `/v1/models` after their connection has a saved key.

## Runtime settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `OPENSWIFTSCALE_ADMIN_USERNAME` | `admin` | Administrator console username. |
| `OPENSWIFTSCALE_LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `OPENSWIFTSCALE_PUBLIC_URL` | empty | Client-reachable gateway origin shown in the console, for example `http://192.168.1.25:8080` or an internal HTTPS URL. |
| `OPENSWIFTSCALE_DATABASE_PATH` | `/data/openswiftscale.db` | SQLite path. |
| `OPENSWIFTSCALE_MASTER_KEY_PATH` | `/data/keys/master.key` | Master key stored separately from SQLite. Generated with mode `0600` on first start. |
| `OPENSWIFTSCALE_MASTER_KEY` or `_FILE` | empty | Optional externally managed URL-safe base64 encoding of exactly 32 random bytes. |
| `OPENSWIFTSCALE_CATALOG_PATH` | `/etc/openswiftscale/catalog.yaml` | Model catalog path. |
| `OPENSWIFTSCALE_REQUEST_TIMEOUT_SECONDS` | `120` | Provider request timeout. |
| `OPENSWIFTSCALE_MAX_BODY_BYTES` | `8388608` | Maximum inference request size. |
| `OPENSWIFTSCALE_RATE_LIMIT_RPM` | `60` | Per-key requests per minute; `0` disables the RPM limit. |
| `OPENSWIFTSCALE_CONCURRENCY` | `8` | Per-key concurrent requests; `0` disables the concurrency limit. |
| `OPENSWIFTSCALE_AUTH_DISABLED` | `false` | Explicit local-only authentication bypass. |
| `OPENSWIFTSCALE_LOG_PROMPTS` | `false` | Reserved opt-in. Prompt persistence is not implemented. |

The console uses HTTP Basic authentication for its management API. `scripts/install.sh` generates a random production password in `secrets/admin_password.txt`; `scripts/dev.sh` uses `admin` / `openswiftscale` for local development. Existing installations may temporarily continue using `OPENSWIFTSCALE_MANAGEMENT_TOKEN` as the administrator password while migrating.

## API users and client keys

Use **Console → API access** to create credentials for applications and people calling `/v1/*`. API users are separate from the administrator account and provider connections:

- An API user is an identity and may own multiple keys.
- Disabling a user blocks every key belonging to that user.
- Keys can be created and revoked independently; their full value is shown only once.
- SQLite stores only a SHA-256 hash, prefix, label, creation time, and last-used time for each managed key.
- Deleting a user revokes and removes all keys belonging to that user.

Keys configured through `OPENSWIFTSCALE_API_KEYS` or `OPENSWIFTSCALE_API_KEYS_FILE` remain bootstrap credentials and are not listed in the API access page. They are retained for installation and backward compatibility; managed keys are recommended for normal workloads.

The standard start script detects the host's active LAN address, publishes port `8080` on that address, and sets `OPENSWIFTSCALE_PUBLIC_URL` so the Overview page shows an address other machines can reach. Override `OPENSWIFTSCALE_BIND_ADDRESS` and `OPENSWIFTSCALE_PUBLIC_URL` when using a fixed private DNS name, reverse proxy, VPN address, or internal load balancer.

Port `5173` is only the Vite development server for the web console. It proxies browser development requests to the Go service and must never be advertised as the inference endpoint. Client applications use the Go gateway at the configured `OPENSWIFTSCALE_PUBLIC_URL`, normally `http://<host-lan-ip>:8080/v1`.

## Provider connections

The normal setup path is **Console → Connections**. Official provider protocol, endpoint, authentication, and model settings are seeded into SQLite on first start. Entering or replacing an API key does not perform a network request; the connection is initially marked `unverified`.

Custom endpoints support the OpenAI-compatible and Anthropic Messages protocols. HTTPS is required unless the user explicitly allows HTTP for a trusted local endpoint.

For migration only, `OPENSWIFTSCALE_<PROVIDER>_API_KEY`, its `_FILE` variant, and `OPENSWIFTSCALE_<PROVIDER>_BASE_URL` are still accepted for built-in provider IDs. A legacy key is imported only when the database does not already contain one. New deployments should use the console.

Provider secrets are encrypted with AES-256-GCM using per-secret random nonces and provider-bound associated data. The API never returns a stored credential. Losing the master key makes encrypted Provider API keys unrecoverable, so back it up separately and securely.

## Model catalog

`config/catalog.yaml` is a versioned seed catalog. Provider and model runtime configuration is copied into SQLite on first start and can then be changed without restarting the process. A model is available only when:

1. It exists in the catalog.
2. Its capability includes the requested endpoint.
3. Its provider connection is enabled and has a configured key.

## Multi-route models

Multiple connections can publish the same **Public model ID**. Each connection contributes one route with these controls:

| Setting | Meaning |
| --- | --- |
| Priority | Lower numbers are attempted before higher numbers. The default is `100`. |
| Weight | Relative traffic share among available routes at the same priority. The default is `100`. |

The Public Model ID is the aggregation key. Built-in connections show their Public Model ID and provider-specific Upstream Model ID in the connection dialog. To attach a third-party endpoint to an existing routing group, configure that endpoint with the exact same Public Model ID; its Upstream Model ID may be different. Public IDs on built-in routes are read-only, while priority and weight remain editable.

The Models page provides a four-step routing wizard:

1. Select the exact Public Model ID clients will send in the request.
2. Choose failover, load-balancing, or hybrid behavior.
3. Review existing endpoints, add third-party endpoints, and fine-tune priority and weight.
4. Confirm the resolved endpoint chain and save it.

The wizard changes endpoint routing only for the selected Public Model ID. It does not inspect prompts or silently move ordinary model requests to a different model family.

The Route rules list contains only policies explicitly saved through this wizard. Seeded catalog models remain available for selection but are not presented as user-created routing rules. Removing a route rule removes its policy record while preserving the underlying provider connections and model endpoints.

For example, two routes with priority `10` and weights `80` and `20` receive approximately 80% and 20% of new requests. A third route with priority `20` normally receives no initial traffic; it becomes the next failover target after priority-10 routes fail.

Automatic failover advances on connection and timeout errors and on upstream HTTP `401`, `403`, `408`, `429`, and `5xx` responses. Other client errors are returned directly because retrying the same invalid request against another provider can hide a request problem. The gateway does not persist or replay prompts beyond the current HTTP request.

Successful inference responses include `X-OpenSwiftScale-Provider` and `X-OpenSwiftScale-Route-Priority`, and the Usage page records the provider that ultimately served each request. Responses resolved through a cross-model rule also include `X-OpenSwiftScale-Routing-Rule`, `X-OpenSwiftScale-Resolved-Model`, and `X-OpenSwiftScale-Model-Priority`. These make route switching and failover observable without exposing provider credentials.

## Advanced virtual model aliases

A virtual model alias exposes an Alias ID that clients can use in the OpenAI-compatible `model` field. Its members are different Public Model IDs, each with a priority and weight. Resolution has two levels:

1. The rule selects a member model by member priority and weight.
2. That model selects a provider endpoint by route priority and weight.

If every endpoint for a member model fails, the request advances to the next eligible member. Equal-priority members provide cross-model traffic distribution. Rule IDs must not duplicate Public Model IDs, rules cannot contain other rules, and every member must reference an existing model.

Price fields are local estimates, not provider invoices. Verify prices and model limits before production deployment.

## Persistence and backups

Local development uses `data/openswiftscale.db`; Docker uses `/data/openswiftscale.db` in the persistent `openswiftscale_data` volume. Configuration changes take effect from SQLite and survive process or container restarts. The YAML catalog remains seed data and does not overwrite operator configuration on every start.

Back up SQLite together with the matching master encryption key before upgrades. See [Data and storage](DATA_AND_STORAGE.md) for stored fields, non-persisted prompt data, consistent backup, restore, reset, and schema migration details.

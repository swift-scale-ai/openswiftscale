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

## Client API keys

Use **API keys** in the top navigation to create credentials for applications calling `/v1/*`:

- Keys can be created and revoked independently; their full value is shown only once.
- SQLite stores only a SHA-256 hash, prefix, label, creation time, and last-used time for each managed key.

The console intentionally presents a flat key list. The current database retains an internal key-owner record for backward-compatible storage, but users do not need to create or manage that record.

Keys configured through `OPENSWIFTSCALE_API_KEYS` or `OPENSWIFTSCALE_API_KEYS_FILE` remain bootstrap credentials and are not listed in the API access page. They are retained for installation and backward compatibility; managed keys are recommended for normal workloads.

The standard start script detects the host's active LAN address, publishes port `8080` on that address, and sets `OPENSWIFTSCALE_PUBLIC_URL` to an address other machines can reach. Override `OPENSWIFTSCALE_BIND_ADDRESS` and `OPENSWIFTSCALE_PUBLIC_URL` when using a fixed private DNS name, reverse proxy, VPN address, or internal load balancer.

Port `5173` is only the Vite development server for the web console. It proxies browser development requests to the Go service and must never be advertised as the inference endpoint. Client applications use the Go gateway at the configured `OPENSWIFTSCALE_PUBLIC_URL`, normally `http://<host-lan-ip>:8080/v1`.

## Provider connections

The normal setup path is the console's **Models** workspace. Select an exact model ID and configure one of the endpoints displayed on the right. Official provider protocol, endpoint, authentication, and model settings are seeded into SQLite on first start. Entering or replacing an API key does not perform a network request; the connection is initially marked `unverified`.

Custom endpoints support the OpenAI-compatible and Anthropic Messages protocols. HTTPS is required unless the user explicitly allows HTTP for a trusted local endpoint.

For migration only, `OPENSWIFTSCALE_<PROVIDER>_API_KEY`, its `_FILE` variant, and `OPENSWIFTSCALE_<PROVIDER>_BASE_URL` are still accepted for built-in provider IDs. A legacy key is imported only when the database does not already contain one. New deployments should use the console.

Provider secrets are encrypted with AES-256-GCM using per-secret random nonces and provider-bound associated data. The API never returns a stored credential. Losing the master key makes encrypted Provider API keys unrecoverable, so back it up separately and securely.

## Model catalog

`config/catalog.yaml` is a versioned seed catalog. Provider and model runtime configuration is copied into SQLite on first start and can then be changed without restarting the process. A model is available only when:

1. It exists in the catalog.
2. Its capability includes the requested endpoint.
3. Its provider connection is enabled and has a configured key.

The top-level `families` list is discovery metadata for the console's publisher → family hierarchy. Each family references its official `provider`, so the console can show the publisher-operated endpoint even before an exact model ID is configured. A family with `routeable: false` is publicly offered by its publisher but uses a dedicated image, video, speech, music, or moderation protocol that this gateway does not yet implement. It remains visible for catalog completeness, but it is never advertised by `/v1/models` and cannot receive traffic until an exact model route and protocol adapter exist.

## Same-model endpoint routing

Multiple connections can publish the same exact **model ID**. The main workspace groups those endpoints together and displays their order, provider, address, price metadata, and readiness.

| Setting | Meaning |
| --- | --- |
| Priority | Lower numbers are attempted before higher numbers. The default is `100`. |
| Weight | Relative traffic share among available routes at the same priority. The default is `100`. |

The exact model ID is the aggregation key. To attach a third-party endpoint, configure it with the same model ID; its provider-specific upstream identifier may differ. Endpoint routing must never substitute a different model ID.

The simplified console does not expose failover, load-balancing, hybrid, or virtual-alias terminology. Use **Route settings** beside the selected exact model ID to enable or exclude endpoints, drag them into fallback order, and set their initial traffic weights. In manual mode, weights select the first endpoint and the dragged order determines subsequent attempts after a retryable failure. Enabling **Use platform default strategy** restores equal default priority and weight while preserving the selected participating endpoints.

For example, two routes with priority `10` and weights `80` and `20` receive approximately 80% and 20% of new requests. A third route with priority `20` normally receives no initial traffic; it becomes the next failover target after priority-10 routes fail.

Automatic failover advances on connection and timeout errors and on upstream HTTP `401`, `403`, `408`, `429`, and `5xx` responses. Other client errors are returned directly because retrying the same invalid request against another provider can hide a request problem. The gateway does not persist or replay prompts beyond the current HTTP request.

Successful inference responses include `X-OpenSwiftScale-Provider` and `X-OpenSwiftScale-Route-Priority`, and request records identify the provider that ultimately served each request.

## Deprecated cross-model configuration

Older builds exposed virtual model aliases and model fallback chains. They are no longer part of the product interface because they can replace the model explicitly selected by a developer. Existing database records and compatibility APIs are retained during the interface transition and will be addressed by a separate data migration.

Price fields are local estimates, not provider invoices. Verify prices and model limits before production deployment.

## Persistence and backups

Local development uses `data/openswiftscale.db`; Docker uses `/data/openswiftscale.db` in the persistent `openswiftscale_data` volume. Configuration changes take effect from SQLite and survive process or container restarts. The YAML catalog remains seed data and does not overwrite operator configuration on every start.

Back up SQLite together with the matching master encryption key before upgrades. See [Data and storage](DATA_AND_STORAGE.md) for stored fields, non-persisted prompt data, consistent backup, restore, reset, and schema migration details.

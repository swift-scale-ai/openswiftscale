# Data and storage

OpenSwiftScale Community uses SQLite as its runtime source of truth. It does not require PostgreSQL, Redis, or a hosted SwiftScale account.

## Database locations

| Environment | Database |
| --- | --- |
| Local source development | `data/openswiftscale.db` |
| Docker Compose | `/data/openswiftscale.db` inside the `openswiftscale_data` named volume |
| Custom binary deployment | `OPENSWIFTSCALE_DATABASE_PATH` |

SQLite WAL mode is enabled. A running database may therefore have `-wal` and `-shm` companion files. Do not copy only the main file while writes are active.

## What is stored

- Official and third-party provider connections.
- Public model definitions and provider-specific upstream model IDs.
- Endpoint priority, weight, capability, and enabled state.
- Explicit model route policies and advanced virtual aliases.
- API users and their enabled state.
- Managed Gateway API-key labels, prefixes, SHA-256 hashes, creation time, and last-used time.
- Request metadata including request ID, model, serving provider, status, token counts, latency, and local estimated cost.
- Provider API keys as AES-256-GCM ciphertext with unique nonces.

## What is not stored

- Prompt or response bodies.
- Plaintext managed Gateway API keys; the full value is returned once when a key is created.
- Plaintext provider credentials.
- Administrator passwords in SQLite.
- Browser session credentials outside the current browser tab.
- SwiftScale Cloud account or telemetry data.

Bootstrap Gateway API keys and the administrator password are file-backed secrets created by the installer under `secrets/`. They are mounted into the container and are intentionally separate from SQLite.

## Encryption key

Provider credentials are encrypted with the master key at `/data/keys/master.key` by default. The key is not stored in SQLite. A database backup without the matching master key cannot decrypt provider credentials; a host compromise containing both files can.

Back up the database and master key separately and protect them with different access controls. Never commit either file.

## Backup and restore

For a consistent single-node backup, stop the gateway first:

```bash
./scripts/stop.sh
```

Back up both the Docker volume data and master key, then restart with:

```bash
./scripts/start.sh
```

For local source development, copy `data/openswiftscale.db` and the configured master-key file while the development process is stopped. Restore both files to their original paths and permissions before starting the same or a compatible newer version.

Test restores regularly. A backup is not complete unless provider credentials can still be decrypted and `/readyz` reports ready.

## Resetting local state

Stopping a process does not delete its database. To create a clean local development state, stop OpenSwiftScale and move `data/openswiftscale.db` plus any `-wal` and `-shm` files to a recoverable backup location. The next start creates a new database and seeds the current catalog.

Removing a Docker volume permanently deletes configuration, API users, usage history, and encrypted credentials. Treat volume removal as destructive and make a backup first.

## Schema upgrades and retention

Schema migrations run automatically during startup and are designed to preserve existing data. Back up before upgrading. Community Edition currently keeps usage metadata until the operator removes or archives it; it does not persist prompts or responses. Long-term retention policies and immutable audit export are Enterprise concerns.


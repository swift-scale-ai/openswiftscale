# Security policy

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Send a minimal report to `security@swift-scale.com` with the affected version, reproduction steps, impact, and any suggested mitigation. Please do not include live provider credentials or private prompts.

## Secure deployment baseline

- Bind the default console to localhost or place it behind TLS and trusted network controls.
- Protect and back up `/data/keys/master.key` separately from the SQLite database, or inject `OPENSWIFTSCALE_MASTER_KEY_FILE` from an external secret manager.
- Use different random values for Gateway API keys, provider credentials, and the administrator password.
- Create separate managed API users and keys for each workload; revoke unused keys and review last-used timestamps.
- Keep prompt logging disabled.
- Pin production container images by digest.
- Back up the SQLite volume and test restoration.
- Restrict outbound traffic to explicitly configured provider endpoints.
- Review the model catalog, prices, limits, and regional endpoints before deployment.
- Rotate provider keys independently of gateway client keys.
- Never expose a management console containing provider configuration directly to the public internet.
- Treat custom provider endpoints as privileged configuration because they control server-side outbound requests.

Provider API keys entered in the console are encrypted with AES-256-GCM before SQLite persistence. The management API reports only whether a key exists and never returns plaintext or ciphertext. Encryption protects a database file or database-only backup; it does not protect a host where both SQLite and the master key have been compromised.

Managed Gateway API keys are never encrypted because the server does not need to recover them. The full key is displayed once at creation; SQLite stores a SHA-256 hash and a short safe prefix. The installer-created bootstrap key remains in a mounted secret file for initial access and backward compatibility. Treat that file as a privileged credential and prefer managed keys for normal applications.

Prompt and response bodies are not written to SQLite. Usage records contain request IDs, model and provider identifiers, status, token counts, latency, estimated cost, and timestamps. These metadata may still be sensitive in some organizations and should follow the same backup and access controls as the rest of the database.

OpenSwiftScale never needs a SwiftScale account and does not emit product telemetry by default.

See [Data and storage](docs/DATA_AND_STORAGE.md) for the complete persistence and backup model.

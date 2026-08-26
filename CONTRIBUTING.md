# Contributing

Thank you for helping improve OpenSwiftScale.

## Before starting

1. Search existing issues and pull requests.
2. Open an issue before a large protocol, persistence, security, or architecture change.
3. Keep Community functionality independent of SwiftScale Cloud and commercial licensing services.
4. Add provider behavior only when an official API contract or a clearly documented compatible protocol exists.

## Local checks

Follow the [development guide](docs/DEVELOPMENT.md), then run:

```bash
npm --prefix frontend run build
make check
```

Add tests for request rewriting, streaming, authentication, API-key lifecycle, routing, persistence migrations, and usage extraction when those areas change.

## Pull-request expectations

- Keep a pull request focused and explain its user-visible behavior.
- Preserve backward compatibility or document a migration path.
- Update English documentation and all supported console translations for user-facing changes.
- Never commit provider credentials, Gateway API keys, administrator passwords, prompts, responses, customer data, master keys, generated SQLite files, or database WAL files.
- Do not add telemetry, an account requirement, or a mandatory hosted dependency to Community Edition.
- Verify new dependencies have a compatible license and a clear operational purpose.
- Use `gofmt`; do not hand-edit generated frontend build artifacts except through the normal build.

## Licensing

By contributing, you agree that your contribution is licensed under Apache-2.0.

Do not submit material you do not have the right to license. OpenSwiftScale and SwiftScale trademarks are not granted by the source-code license; see the [open-source policy](docs/OPEN_SOURCE.md).

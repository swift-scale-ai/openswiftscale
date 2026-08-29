# Open-source policy

OpenSwiftScale is free and open-source software licensed under the Apache License 2.0. It remains independently useful without a hosted account, commercial license, external control plane, or mandatory telemetry.

## What the license permits

Subject to the full [Apache License 2.0](../LICENSE), users may use, reproduce, modify, distribute, and create derivative works from the source, including for commercial and internal purposes. The license also includes the patent grant and redistribution conditions stated in the license text.

This document is explanatory and does not replace the license.

## Open-source guarantees

The open-source edition does not impose product-level limits on requests, tokens, providers, keys, endpoint pools, or local deployments. The public repository includes:

- OpenAI-compatible inference APIs.
- Direct provider and local compatible endpoints.
- Directly managed, revocable Gateway API keys.
- Transparent same-model endpoint ordering and automatic failover.
- Local usage metadata, estimated cost, latency, and health information.
- Embedded multilingual management console.
- SQLite persistence, Docker deployment, CI, release binaries, container images, SBOMs, and provenance.

OpenSwiftScale Cloud uses the same brand, model hierarchy, exact-model routing behavior, and API contract. It is a managed superset; it does not turn the self-hosted edition into a time-limited trial or add a mandatory Cloud dependency.

## Contributions

Contributions intentionally submitted to this repository are accepted under Apache-2.0 unless explicitly stated otherwise. Contributors retain copyright in their work while granting the rights described by the project license. See [Contributing](../CONTRIBUTING.md) before opening a pull request.

No contributor should submit code, documentation, model metadata, or test data they do not have the right to license.

## Trademarks and branding

The Apache license does not grant trademark rights. “OpenSwiftScale”, its product logos, and related branding identify their respective owners. Forks may accurately describe their origin and retain legally required notices, but should not imply endorsement or official status. Use a distinct name and branding for a redistributed product unless permission has been granted.

## Third-party software and model services

Dependencies keep their own licenses and notices. Model weights, provider APIs, SDKs, model names, and service terms are not relicensed by this repository. Operators are responsible for reviewing provider terms, data-processing rules, regional restrictions, model licenses, and pricing.

OpenSwiftScale does not sell model credits and does not make an unavailable or restricted model open source merely by listing compatible metadata.

## Security and support

Apache-2.0 software is provided without warranty. Report vulnerabilities privately as described in [Security](../SECURITY.md). Questions and reproducible bugs may be filed through GitHub Issues.

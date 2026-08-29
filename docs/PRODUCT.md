# OpenSwiftScale product design

## Product statement

OpenSwiftScale is a free, open-source, self-hosted AI gateway for individual developers and small teams. It provides one OpenAI-compatible endpoint while making the complete request path understandable: model publisher, model family, exact model ID, and every endpoint capable of serving it.

OpenSwiftScale is an independent product and brand. It does not require a hosted account, license server, external control plane, or mandatory telemetry.

## Design principles

1. **Useful in three minutes.** Install, add one provider key, create one Gateway API key, test, and finish.
2. **One transparent workspace.** The primary interface follows publisher → family → exact model ID → endpoints.
3. **The requested model is immutable.** Routing may select another endpoint for the same exact model, but must never silently substitute a different model.
4. **Simple defaults, visible control.** OpenSwiftScale chooses a sensible endpoint order; users may override that order without learning a routing language.
5. **Local control is real.** Credentials, request metadata, and operational records stay inside the user-selected network path.
6. **Small default footprint.** One process, one container, embedded UI, and SQLite.
7. **Protocols before proprietary SDKs.** OpenAI-compatible HTTP is the primary client contract.
8. **Secure defaults.** Separate data-plane and management credentials, no prompt logging, no telemetry, and a non-root container.

## Target users

- Individual developers using several commercial, open-weight, or local model providers.
- Small product teams that need one inexpensive internal AI endpoint.
- Developers who want to understand exactly which provider endpoint served a request.

Enterprise governance, organization hierarchies, SSO/SCIM, approval workflows, and fleet management are intentionally outside this product.

## Open-source scope

- Direct provider connections and BYOK.
- A model catalog organized by publisher, family, exact model ID, and endpoint.
- Same-model endpoint ordering and failover with no cross-model substitution.
- OpenAI-compatible chat, responses, embeddings, and model discovery.
- Streaming, tool calls, structured-output pass-through, and provider error handling.
- Independently revocable, hash-only Gateway API keys and separate administrator authentication.
- Local request metadata, Token usage, estimated cost, latency, and serving endpoint.
- Embedded web console, health, readiness, and Prometheus metrics.
- Docker Compose and multi-architecture release artifacts.
- Multilingual onboarding and concise SDK examples.

## Explicit non-goals

- Semantic task classification or automatic model selection.
- Virtual model aliases that route across different model IDs.
- Enterprise identity, policy, compliance, or multi-organization administration.
- Acting as a model reseller or credit wallet in the self-hosted edition.
- Depending on OpenRouter or another model aggregator.
- Storing prompts or responses by default.
- Claiming support for a model without a verifiable provider contract.

## Open-source and Cloud consistency

The self-hosted and Cloud editions use the same OpenSwiftScale brand, model hierarchy, exact-model routing invariant, API contract, and visual language. Cloud is a managed superset that adds hosted accounts, balance and payments, platform-operated endpoints, regional availability, observed quality metrics, and a clearly disclosed service fee. Cloud-only concerns do not add control-plane dependencies to the open-source runtime.

## Success criteria

- First request in under three minutes after Docker is available.
- A new user can understand the main screen without reading routing documentation.
- Every request record identifies the requested model and serving endpoint.
- The gateway never changes the exact requested model ID during failover.
- Idle memory target below 100 MB for the default process.
- Gateway processing overhead target below 5 ms p95, excluding provider time.
- No outbound network calls except providers explicitly configured by the operator.

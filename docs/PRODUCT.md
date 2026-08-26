# OpenSwiftScale product design

## Product statement

OpenSwiftScale is a free, open-source, self-hosted AI gateway for developers and organizations that want a simple OpenAI-compatible endpoint while retaining control over model credentials, network paths, request data, and operational records.

The Community product must remain useful without an account, license server, hosted control plane, or mandatory telemetry. Its adoption builds the SwiftScale brand and creates a natural path to enterprise governance and managed SwiftScale services.

## Design principles

1. **Useful in three minutes.** A developer should move from clone to first request with Docker Compose and one provider key.
2. **Local control is real.** Inference data and credentials stay inside the user-selected network path.
3. **No forced cloud dependency.** SwiftScale Cloud integrations are optional and opt-in.
4. **Small default footprint.** One process, one container, embedded UI, SQLite.
5. **Protocols before proprietary SDKs.** OpenAI-compatible HTTP is the primary client contract.
6. **Transparent routing.** Every public model maps to inspectable provider routes with explicit priorities, weights, and failover behavior.
7. **Secure defaults.** Separate data-plane and management credentials, no prompt logging, no telemetry, non-root container.
8. **Community is not a trial.** Core routing, observability, and direct provider access remain free.

## Target users

- Individual developers using several commercial or local model providers.
- Startup teams that need one internal AI endpoint and local cost visibility.
- Enterprise platform teams evaluating a controlled internal AI gateway.
- Regulated teams that cannot send gateway telemetry to an external SaaS.

## Community scope

- Direct provider connections and BYOK.
- OpenAI-compatible chat, responses, embeddings, and model discovery.
- Streaming, tool calls, structured output pass-through, and provider error handling.
- Versioned model catalog, multi-route priorities, weighted traffic, and explicit fallbacks.
- A guided model-route wizard for failover, load-balancing, and hybrid endpoint pools.
- Managed API users, independently revocable hash-only Gateway API keys, and separate administrator authentication.
- Local request metadata, token usage, estimated cost, and latency.
- Embedded web console, health, readiness, and Prometheus metrics.
- Docker Compose and multi-architecture release artifacts.
- Multilingual developer onboarding with callable model IDs and cURL, Python, Go, Node.js, Java, and Rust examples.

## Explicit non-goals for the first release

- Acting as a model reseller or credit wallet.
- Depending on OpenRouter or another model aggregator.
- Global traffic management or multi-region settlement.
- Storing prompts or responses by default.
- Reproducing SwiftScale's private smart-routing and commercial billing systems.
- Semantic task classification and organization-wide request-policy routing; these belong to Enterprise and SwiftScale Cloud.
- Claiming support for a model without an official direct API contract.

## Success criteria

- First request in under three minutes after Docker is available.
- Idle memory target below 100 MB for the default process.
- Gateway processing overhead target below 5 ms p95, excluding provider time.
- No outbound network calls except providers explicitly configured by the operator.
- Community users can build, audit, run, and upgrade without contacting SwiftScale.

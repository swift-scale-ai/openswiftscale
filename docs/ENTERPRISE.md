# Community and enterprise model

## Community Edition

Community Edition is Apache-2.0 software and remains fully functional without a commercial license. It does not impose token, request, provider, API-user, API-key, endpoint-pool, or deployment limits. Core direct-provider routing, local observability, Docker deployment, and the multilingual console stay in the public repository.

Community routing is deterministic and inspectable: an exact Public Model ID selects its endpoint pool, where priority, weight, and request-local failover control provider selection. The routing wizard, unlimited endpoint pools, managed local API users and keys, and advanced static virtual model aliases remain part of Community Edition.

## Enterprise Edition

Enterprise value should focus on organization-wide governance and operational guarantees:

- SAML/OIDC SSO and SCIM provisioning.
- Fine-grained RBAC and separation of duties.
- Central fleet and configuration management.
- Task-aware and request-aware routing based on prompt classification, headers, tenant, team, region, budget, latency, data policy, or model-quality signals.
- Adaptive routing decisions, organization-wide circuit-breaker coordination, policy versioning, approvals, and routing-decision audit trails.
- Approval workflows and signed configuration releases.
- Long-term immutable audit export.
- Data-residency and egress policy enforcement.
- DLP, PII, and policy packs.
- Air-gapped installation and private registry lifecycle.
- High-availability coordination and disaster recovery guidance.
- LTS security updates, SLA, and commercial support.

Self-hosted enterprise pricing should be based on an organization or managed gateway fleet, not a tax on tokens sent to customer-owned providers.

## SwiftScale Cloud

SwiftScale Cloud is a separate optional managed service for global access, managed inference, commercial billing, provider health, and proprietary routing intelligence. Connecting a Community gateway to SwiftScale Cloud must be explicit and reversible; disconnecting it must not disable local operation.

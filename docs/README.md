# Documentation

TktSync documentation is organized around durable system concerns. The governing documents define current behavior; the implementation history preserves milestone terminology and release evidence without acting as a current architectural authority.

## Where do I read about X?

Start with these documents in order. Each document states its own normative parents where further precedence applies.

1. [Platform Policy](architecture/platform-policy.md) — authoritative operating rules, responsibility boundaries, and platform invariants.
2. [Logical Domain Model](architecture/domain-model.md) — domain concepts, state machines, ownership, and authorization semantics.
3. [System Architecture and Transactional Design](architecture/system-design.md) — authority boundaries, transactions, concurrency, failure behavior, and deployment topology.
4. [Relational Data Model](architecture/data-model.md) — PostgreSQL schema, constraints, locking, migrations, and persistence invariants.
5. [Security and Authentication](architecture/security.md) — identities, authorization, credentials, secrets, and security controls.
6. [Realtime and Event Contract](architecture/realtime-events.md) — outbox events, browser realtime, Partner webhooks, and delivery semantics.
7. [Technology Stack](architecture/technology-stack.md) — approved implementation technologies and trade-offs.

For product scope and responsibility boundaries, start with Platform Policy. For a Reservation, Ticket, Admission, or other lifecycle concept, use the Domain Model. For transaction boundaries, locking, retries, and degradation behavior, use System Design. For tables, constraints, and migration rules, use the Data Model. For identities, credentials, capabilities, and logging rules, use Security. For realtime/webhook payload behavior, use the Realtime Contract. For implementation technology choices, use Technology Stack.

## API and Partner integration

- [API and Partner Integration Contract](api-contract.md) — external HTTP behavior, Partner obligations, error semantics, idempotency, and compatibility rules.
- [OpenAPI definition](../openapi/tktsync.v1.json) — machine-readable production API contract.

Use the contract for semantics and Partner responsibilities; use OpenAPI for exact routes, schemas, parameters, and response media types.

## Operations

- [Production Runtime Model](operations/runtime-model.md) — process topology, real concurrency defaults, horizontal scaling, database-pool budgeting, shutdown, failure behavior, and observability.
- [MVP Release Runbook](operations/release-runbook.md) — deployment, rollback, backup, recovery, key rotation, and alert response.
- [Release Traceability](operations/release-traceability.md) — release requirements mapped to executable evidence.
- [Local Smoke Checklist](operations/local-smoke-checklist.md) — short local startup and product-flow verification.

## Implementation history

- [Implementation Plan and History](implementation-history.md) — the original M0–M11 sequence, milestone gates, and definitions of done. Milestone names are retained as historical traceability terms.

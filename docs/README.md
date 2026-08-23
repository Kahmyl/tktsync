# Documentation

TktSync documentation is organized around durable system concerns. The governing documents define current behavior; the implementation history preserves milestone terminology and release evidence without acting as a current architectural authority.

## Architecture and governance

Start with these documents in order. Each document states its own normative parents where further precedence applies.

1. [Platform Policy](architecture/platform-policy.md) — authoritative operating rules, responsibility boundaries, and platform invariants.
2. [Logical Domain Model](architecture/domain-model.md) — domain concepts, state machines, ownership, and authorization semantics.
3. [System Architecture and Transactional Design](architecture/system-design.md) — authority boundaries, transactions, concurrency, failure behavior, and deployment topology.
4. [Relational Data Model](architecture/data-model.md) — PostgreSQL schema, constraints, locking, migrations, and persistence invariants.
5. [Security and Authentication](architecture/security.md) — identities, authorization, credentials, secrets, and security controls.
6. [Realtime and Event Contract](architecture/realtime-events.md) — outbox events, browser realtime, Partner webhooks, and delivery semantics.
7. [Technology Stack](architecture/technology-stack.md) — approved implementation technologies and trade-offs.

## API and integration

- [API and Partner Integration Contract](api-contract.md) — external HTTP behavior, Partner obligations, error semantics, idempotency, and compatibility rules.
- [OpenAPI definition](../openapi/tktsync.v1.json) — machine-readable production API contract.

## Operations

- [MVP Release Runbook](operations/release-runbook.md) — deployment, rollback, backup, recovery, key rotation, and alert response.
- [Release Traceability](operations/release-traceability.md) — release requirements mapped to executable evidence.

## Implementation history

- [Implementation Plan and History](implementation-history.md) — the original M0–M11 sequence, milestone gates, and definitions of done. Milestone names are retained as historical traceability terms.

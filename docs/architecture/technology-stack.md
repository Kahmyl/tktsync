# Technology Stack

> Approved implementation stack and engineering trade-offs for the TktSync MVP.

---

## 1. Purpose

This document records the approved implementation stack for TktSync and the engineering trade-offs behind the selection.

The stack is intended to support:

- high-concurrency inventory operations;
- low-latency partner and scanner interactions;
- explicit PostgreSQL transaction control;
- reliable background processing;
- simple deployment and operational ownership;
- clear frontend/backend separation;
- one authoritative implementation of the TktSync domain model.

---

## 2. Approved Stack

| Surface | Technology |
|---|---|
| Admin Web | React + Vite |
| White-label Buyer Selector | React + Vite |
| Scanner Web | React + Vite |
| Authoritative Backend | Go |
| API Runtime | Go |
| Background Worker | Go |
| Transactional Database | PostgreSQL via Supabase |
| Realtime Infrastructure | Supabase Realtime / outbox-driven realtime |
| Repository Model | Monorepo |

The API and background worker are separate deployable binaries built from the same Go backend codebase and shared domain/application packages.

---

## 3. Frontend: React + Vite

React + Vite is used for all four web applications:

- administrative dashboard;
- white-label buyer selector;
- scanner interface.
- Partner API documentation.

### Rationale

React provides a mature component model and ecosystem suitable for the interactive interfaces required by TktSync, particularly seat selection, live inventory presentation, administration, and scanning workflows.

Vite provides a lightweight frontend build/runtime model without introducing server-rendering infrastructure that these applications do not require.

Using the same frontend stack across all four applications also allows common UI primitives, API clients, validation helpers, and design-system packages to be shared inside the monorepo.

### Accepted Trade-offs

The frontend does not share backend implementation types directly because the authoritative backend is written in Go.

This is intentional. Frontend/backend compatibility is maintained through versioned API contracts and generated TypeScript clients rather than coupling frontend code to backend domain implementation.

---

## 4. Backend: Go

Go is the single authoritative backend implementation language.

It owns:

- Venue and Event operations;
- Inventory;
- Reservations and checkout protection;
- Blocks and Allocations;
- Sale confirmation;
- Ticketing and QR credentials;
- Admission and scanning;
- Partner authorization;
- Audit and transactional outbox;
- background expiry and reconciliation processing.

### Rationale

TktSync contains several latency-sensitive and concurrency-heavy paths, including:

- high-contention ticket holds;
- General Admission quantity acquisition;
- partner confirmation;
- scanner validation;
- background expiry and reconciliation;
- realtime/outbox processing.

Go provides a lightweight concurrency model, efficient network handling, predictable runtime characteristics, and a strong fit for both API and worker workloads.

More importantly, using Go for the complete authoritative backend keeps all TktSync transactional rules in one implementation.

The same domain and persistence code can therefore enforce:

- canonical lock ordering;
- PostgreSQL transactions;
- reservation expiry semantics;
- allocation-source restoration;
- idempotency;
- ticket issuance;
- audit;
- admission invariants.

This avoids maintaining two separate implementations of the same business rules.

### Accepted Trade-offs

Using Go means frontend and backend types cannot be shared directly as TypeScript source.

The project therefore requires disciplined contract generation and a clear API specification.

Go also generally requires more explicit application and data-access design than highly opinionated application frameworks. This is considered beneficial for TktSync's authoritative transaction paths, where lock acquisition, transaction boundaries, SQL behavior, and error handling must remain visible and deliberate.

---

## 5. API and Worker Separation

The Go backend is deployed as at least two executable processes:

~~~text
Go Codebase
   ├── API
   └── Worker
~~~

The **API** handles synchronous commands and queries from partners, administrators, buyers, and scanners.

The **Worker** handles asynchronous responsibilities such as:

- reservation expiry materialization;
- reconciliation timeout processing;
- outbox dispatch;
- projection maintenance;
- scheduled maintenance.

Both use the same domain and persistence packages.

### Rationale

This provides independent operational scaling without creating separate business services or duplicated domain implementations.

API traffic and background workload can therefore scale independently while preserving one authoritative transactional model.

---

## 6. PostgreSQL / Supabase

PostgreSQL is the authoritative transactional data store.

Supabase provides the managed PostgreSQL environment and supporting realtime/authentication capabilities where appropriate.

### Rationale

TktSync correctness depends primarily on database-level transactional guarantees rather than application-language concurrency alone.

PostgreSQL provides the required mechanisms:

- ACID transactions;
- row-level locking;
- explicit lock ordering;
- uniqueness constraints;
- partial unique indexes;
- check constraints;
- deferred constraint triggers;
- authoritative server time.

These capabilities directly support TktSync's core requirements, including prevention of overselling and duplicate admission.

Supabase allows these PostgreSQL capabilities to be retained while reducing infrastructure overhead for the MVP.

### Accepted Trade-offs

PostgreSQL remains the primary consistency boundary and may become the contention point for highly contested inventory.

This is expected and intentional: contention must be serialized somewhere when multiple buyers compete for the same inventory.

Application-level concurrency must not bypass the database's authoritative ordering.

---

## 7. Monorepo

The implementation is maintained in one repository.

Representative structure:

~~~text
apps/
  admin-web/
  selector-web/
  scanner-web/

backend/
  cmd/
    api/
    worker/
  internal/
    venue/
    event/
    inventory/
    reservation/
    allocation/
    ticketing/
    admission/
    partner/
    audit/
    outbox/

packages/
  api-client/
  contracts/
  ui/

migrations/
~~~

### Rationale

The monorepo keeps:

- frontend applications;
- backend binaries;
- generated contracts;
- shared UI components;
- migrations;
- integration tests

under one versioned development boundary.

It enables coordinated changes without turning frontend and backend implementation details into the same runtime concern.

---

## 8. Contract Boundary

Go remains the authoritative domain implementation.

React applications consume explicit external contracts.

The intended flow is:

~~~text
Go API
   ↓
OpenAPI / versioned contract
   ↓
Generated TypeScript client
   ↓
React applications
~~~

This keeps the applications strongly aligned while preserving language independence.

The API contract—not shared source-language types—is the compatibility boundary.

---

## 9. Governing Engineering Principle

The selected stack follows one central rule:

> **Use one authoritative backend implementation for all transactional TktSync behavior, while allowing independently deployable API, worker, and frontend processes to scale according to their own workload.**

Go provides the backend runtime and concurrency headroom.

PostgreSQL provides transactional correctness.

React + Vite provides the application surfaces.

The monorepo keeps the complete system coordinated without weakening those boundaries.

---

**End of Document**

# Implementation Plan and History

> Historical MVP implementation sequence, milestone gates, and verification record. The milestone terminology is retained for traceability; the governing architecture and policy documents define current system behavior.

Normative implementation inputs:

1. TktSync Technical Brief
2. [Platform Policy](architecture/platform-policy.md)
3. [Logical Domain Model](architecture/domain-model.md)
4. [System Architecture and Transactional Design](architecture/system-design.md)
5. [Relational Data Model](architecture/data-model.md)
6. [Technology Stack](architecture/technology-stack.md)
7. [API and Partner Integration Contract](api-contract.md)
8. [Realtime and Event Contract](architecture/realtime-events.md)
9. [Security and Authentication](architecture/security.md)

---

## 1. Purpose

This document defines the implementation order, milestone boundaries, dependency gates, verification requirements, and MVP completion criteria for TktSync.

The purpose is not to create another interpretation of the system. The governing specifications already define the required behavior.

This document answers:

- what is built first;
- what depends on what;
- what may safely be developed in parallel;
- what must be verified before proceeding;
- which capabilities belong to each milestone;
- what constitutes completion of the TktSync MVP.

Milestones are **dependency and acceptance gates**, not calendar estimates.

A milestone is complete only when its required behavior, tests, migrations, API contract, security controls, and relevant operational behavior pass.

---

## Part I: IMPLEMENTATION PRINCIPLES

## 2. Current Repository Baseline

Implementation begins from a documentation-only repository.

Current baseline:

~~~text
tktsync/
└── docs/
    ├── TktSync_API_and_Partner_Integration_Contract.pdf
    ├── TktSync_Logical_Domain_Specification.md
    ├── TktSync_Platform_Process_and_Policy_Standard.md
    ├── TktSync_Realtime_and_Event_Contract.md
    ├── TktSync_Relational_Data_Model_and_Schema_Specification.md
    ├── TktSync_Security_and_Authentication_Specification.md
    ├── TktSync_System_Architecture_and_Transactional_Design_Specification.md
    └── TktSync_Technology_Stack_Decision_Record.md
~~~

There is no legacy implementation that must be preserved.

The implementation should therefore conform directly to the approved architecture rather than introducing temporary architecture that later requires migration.

---

## 3. Approved Runtime Architecture

~~~text
                       MONOREPO
                          |
         +----------------+----------------+
         |                |                |
         v                v                v
   Admin React/Vite  Selector React/Vite  Scanner React/Vite
         \                |                /
          \               |               /
           +--------------+--------------+
                          |
                          v
                     Go Core API
                          |
                  +-------+-------+
                  |               |
                  v               v
               Go API         Go Worker
                  \               /
                   +------+------+
                          |
                          v
                 PostgreSQL / Supabase
~~~

The Go API and Go Worker are separate deployable executables built from the same authoritative backend codebase.

---

## 4. Repository Target Structure

The implementation should converge toward:

~~~text
docs/

apps/
  admin-web/
  selector-web/
  scanner-web/

backend/
  cmd/
    api/
    worker/

  internal/
    platform/
    auth/
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
    reporting/
    realtime/
    webhook/

packages/
  api-client/
  contracts/
  ui/

migrations/

tests/
  integration/
  concurrency/
  e2e/
  fixtures/
~~~

Exact internal package names may evolve, but domain ownership and runtime boundaries MUST remain consistent with the governing architecture.

---

## 5. One Authoritative Backend

All authoritative business behavior MUST be implemented in Go.

There MUST NOT be separate TypeScript/NestJS ownership of:

- Event lifecycle;
- Inventory;
- Reservations;
- Allocations;
- Confirmation;
- Sales;
- Ticketing;
- QR credentials;
- Admission;
- Partner transaction state.

React applications consume contracts only.

---

## 6. PostgreSQL Is the Correctness Boundary

PostgreSQL determines:

- inventory ownership;
- lock ordering;
- Reservation state;
- GA accounting;
- Sale creation;
- Ticket state;
- QR credential state;
- Admission uniqueness;
- idempotency;
- audit;
- outbox facts.

Go concurrency improves throughput.

It does not replace PostgreSQL transaction semantics.

---

## 7. Contracts Are Implemented Continuously

The API contract MUST NOT be implemented only after the backend is complete.

Every milestone that introduces externally accessible behavior must also include:

1. domain/application implementation;
2. persistence implementation;
3. HTTP handler;
4. authorization;
5. stable business errors;
6. OpenAPI change;
7. generated TypeScript client update where applicable;
8. integration tests.

The OpenAPI contract and implementation must evolve together.

---

## 8. Security Is Continuous

Security is not a final hardening milestone.

Authentication, authorization, secret handling, actor scoping, log hygiene, and token rules are implemented before protected functionality becomes usable.

Later milestones add penetration/security verification but do not introduce basic security for the first time.

---

## 9. Outbox From the Beginning

Every material business transaction that requires an externally observable fact MUST write its OutboxEvent in the same transaction from the first implementation.

External dispatch may be introduced later.

The system MUST NOT initially publish realtime directly from command handlers and later retrofit the outbox.

---

## 10. Risk-First Implementation

Implementation priority favors the parts that can invalidate the entire architecture if wrong:

1. schema;
2. locking;
3. idempotency;
4. inventory accounting;
5. Reservation lifecycle;
6. confirmation;
7. admission;
8. asynchronous delivery;
9. user interfaces.

A polished frontend does not compensate for unresolved inventory correctness.

---

## Part II: MILESTONE OVERVIEW

## 11. Milestone Sequence

~~~text
M0   Scaffold & Setup
 |
 v
M1   PostgreSQL Schema & Persistence Foundation
 |
 v
M2   Go Platform Kernel, Auth, Idempotency, Audit & Outbox
 |
 v
M3   Venue, Layout, Event & Partner Configuration
 |
 v
M4   Inventory, Pricing, Blocks, Allocations & Availability
 |
 v
M5   Reservation, Checkout, Retry, Reconciliation & Expiry
 |
 v
M6   Confirmation, Sale, Ticket, QR & Non-Public Issuance
 |
 v
M7   Admission & Scanner Authority
 |
 v
M8   Realtime, Webhooks & Worker Completion
 |
 v
M9   Complete React Product Surfaces
 |
 v
M10  Reporting, Audit Operations & Accreditation
 |
 v
M11  Concurrency, Security, Partner Certification & MVP Release
~~~

---

## Part III: M0: SCAFFOLD & SETUP

## 12. Objective

Create the complete project skeleton, development environment, runtime scaffolding, and engineering tooling required for implementation to begin cleanly.

M0 contains **no TktSync domain implementation**.

Its purpose is to establish a reproducible working environment in which subsequent milestones can be implemented without repeatedly restructuring the repository or bootstrapping infrastructure.

---

## 13. Monorepo Scaffold

Create the initial repository structure:

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
    platform/
    auth/
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
    reporting/
    realtime/
    webhook/

packages/
  api-client/
  contracts/
  ui/

migrations/

tests/
  integration/
  concurrency/
  e2e/
  fixtures/
~~~

Domain package directories may initially contain only package documentation/placeholders where needed.

Business behavior begins in later milestones.

---

## 14. Go Backend Scaffold

Initialize the Go backend.

Create:

~~~text
backend/go.mod

backend/cmd/api/main.go
backend/cmd/worker/main.go
~~~

Provide baseline infrastructure for:

- configuration loading;
- structured logging;
- context propagation;
- graceful shutdown;
- HTTP server lifecycle;
- worker process lifecycle;
- PostgreSQL connection pool;
- health endpoint;
- readiness endpoint;
- application startup/shutdown hooks.

The API and worker must compile and start independently.

No authoritative Event, Inventory, Reservation, Sale, Ticket, or Admission implementation belongs in M0.

---

## 15. Frontend Scaffold

Initialize:

~~~text
apps/admin-web
apps/selector-web
apps/scanner-web
~~~

Each application uses:

- React;
- Vite;
- TypeScript;
- linting;
- formatting;
- type checking;
- production build configuration.

Each application should initially provide only a basic shell proving that the runtime works.

---

## 16. Shared Frontend Packages

Initialize:

~~~text
packages/ui
packages/contracts
packages/api-client
~~~

### `packages/ui`

Shared visual primitives that genuinely apply across applications.

### `packages/contracts`

Language-level generated/derived frontend contract representations.

This package MUST NOT become a manually maintained alternative source of API truth.

### `packages/api-client`

Generated or generation-ready TypeScript API client package.

The actual production API contract is introduced incrementally with later milestones.

---

## 17. PostgreSQL / Supabase Local Setup

Create a reproducible local PostgreSQL/Supabase-compatible environment.

The setup must support:

- application database connection;
- migrations;
- isolated test database creation;
- future Supabase Auth configuration;
- local developer startup.

No developer should need to manually create production-style tables through a database UI.

---

## 18. Migration Framework

Establish the migration tooling and directory convention.

At M0:

- migration tooling must execute successfully;
- the migrations directory may contain only initialization/bootstrap migrations;
- authoritative business schema arrives in M1.

Production application code MUST NOT create schema dynamically.

---

## 19. Configuration Framework

Establish typed/configured runtime settings for:

~~~text
environment
HTTP server
database
logging
Supabase/Auth configuration
cryptographic keyring configuration placeholders
worker configuration
realtime configuration
webhook configuration
~~~

Add:

~~~text
.env.example
~~~

with safe placeholders only.

Actual secrets must never be committed.

---

## 20. Structured Logging Baseline

Implement structured logging shared by the API and worker.

Baseline fields should support:

~~~text
timestamp
level
service
environment
request_id
correlation_id
operation
error
duration
~~~

Secret-redaction conventions must be established before later milestones introduce credentials.

---

## 21. Health and Readiness

Implement:

~~~text
GET /health
GET /ready
~~~

or equivalent infrastructure endpoints.

### Health

Indicates that the process is running.

### Readiness

Indicates that dependencies required to accept work are available, including PostgreSQL where appropriate.

These endpoints are infrastructure endpoints, not domain API resources.

---

## 22. Graceful Shutdown

Both Go executables must support graceful shutdown.

API:

- stop accepting new requests;
- allow bounded in-flight completion;
- close resources.

Worker:

- stop acquiring new work;
- allow bounded active jobs to complete;
- close database/resources safely.

---

## 23. Local Development Commands

Provide simple root-level developer commands/scripts for common operations.

Examples:

~~~text
dev
dev-api
dev-worker
dev-admin
dev-selector
dev-scanner

test
lint
typecheck
build

db-up
db-down
db-migrate
db-reset
~~~

Exact tooling may use Make, Task, shell scripts, package-manager scripts, or another lightweight approach.

The commands must be documented and reproducible.

---

## 24. Test Harness Scaffold

Create the initial testing structure.

### Go

- unit-test convention;
- integration-test convention;
- database-test helper skeleton.

### Frontend

- component/unit-test foundation where required;
- application test configuration.

### Cross-system

Create placeholders/conventions for:

~~~text
tests/integration
tests/concurrency
tests/e2e
tests/fixtures
~~~

M0 does not require domain tests because no domain behavior exists yet.

---

## 25. Code Quality Tooling

### Go

Establish:

- `gofmt`;
- `go vet`;
- static analysis/linter selection;
- test command.

### TypeScript

Establish:

- ESLint or equivalent;
- formatter;
- TypeScript strict configuration;
- build/typecheck commands.

Formatting and lint rules should be consistent across the monorepo where practical.

---

## 26. CI Pipeline

CI must work from a clean checkout.

At minimum:

### Go

~~~text
format verification
static analysis
unit tests
API build
Worker build
~~~

### React/TypeScript

~~~text
dependency installation
lint
typecheck
Admin build
Selector build
Scanner build
~~~

### Database

~~~text
start clean database
execute migration framework
verify success
~~~

M1 will expand the database verification substantially.

---

## 27. Repository Hygiene

Create/configure:

~~~text
.gitignore
.editorconfig
README.md
.env.example
~~~

README should explain:

- prerequisites;
- local setup;
- environment configuration;
- database startup;
- API startup;
- worker startup;
- frontend startup;
- tests;
- linting;
- builds.

No undocumented manual setup should be required for normal development.

---

## 28. Secret Hygiene

M0 must establish the rule that no real secret is committed.

Exclude:

- `.env`;
- private keys;
- API keys;
- Supabase privileged credentials;
- database passwords;
- signing/HMAC keys;
- webhook secrets;
- generated local secret files.

CI should support secret scanning where practical.

---

## 29. M0 Exit Gate

M0 is complete only when a clean clone can be configured and started without hidden manual setup.

Specifically:

### Repository

- monorepo structure exists;
- documentation remains intact.

### Backend

- Go API compiles;
- Go API starts;
- Go Worker compiles;
- Go Worker starts.

### Database

- local PostgreSQL/Supabase-compatible environment starts;
- API can connect;
- Worker can connect;
- migration command executes successfully.

### Frontend

- Admin application starts/builds;
- Selector application starts/builds;
- Scanner application starts/builds.

### Tooling

- lint passes;
- typecheck passes;
- tests execute;
- CI passes.

### Security

- no actual credentials are committed;
- secret/logging conventions exist.

### Domain boundary

There is no substantive TktSync business implementation yet.

M0 establishes the workshop.

M1 begins building the system.

---

## Part IV: M1: POSTGRESQL SCHEMA & PERSISTENCE FOUNDATION

## 30. Objective

Implement the complete reviewed relational model before application behavior starts depending on informal storage assumptions.

---

## 31. Schema Migrations

Implement migrations for the reviewed schema, including:

### Identity

- `app_users`;
- platform roles;
- Event staff assignments.

### Venue

- venues;
- Venue layout versions;
- sections;
- rows;
- tables;
- seats;
- GA zones.

### Event

- Events;
- Event transaction policies;
- Event layout snapshots;
- Event sections;
- Event pricing;
- reserved inventory units;
- GA pools;
- shared GA inventory.

### Partner

- Partners;
- Partner credentials;
- PartnerEventAccess;
- BuyerSelectionSessions.

### Restrictions

- Blocks;
- Allocations;
- reserved membership;
- GA block buckets;
- GA Allocation buckets.

### Reservation

- Reservations;
- ReservationItems;
- CheckoutAttempts.

### Commercial

- Sales;
- SaleItems;
- NonPublicIssuances;
- NonPublicIssuanceItems;
- TicketEntitlements;
- QRCredentials.

### Inventory Claims

- ReservedInventoryClaims.

### Admission

- ScanAttempts;
- Admissions.

### Infrastructure

- idempotency operations;
- AuditEvents;
- OutboxEvents;
- Partner webhook configuration;
- webhook signing secrets;
- webhook deliveries;
- webhook delivery attempts where used.

---

## 32. Cryptographic Key Version Columns

Implement persistence required by the security specification:

~~~text
buyer_selection_sessions.token_key_version
reservations.continuation_token_key_version
qr_credentials.token_key_version
~~~

---

## 33. Core Constraints

Implement database constraints including:

### Reserved inventory

~~~text
one active claim per ReservedInventoryUnit
~~~

### GA

~~~text
no negative quantity
exact pool balance at transaction commit
active Reservation accounting reconciles with bucket counters
~~~

### Reservation

~~~text
valid lifecycle values
bounded deadlines
one active CheckoutAttempt
~~~

### Sale

~~~text
one Sale per Reservation
SaleItem corresponds to ReservationItem
~~~

### Ticket

~~~text
exact origin
root Ticket cardinality
one active entitlement per reserved unit where applicable
one active QR credential
active QR requires active Ticket
~~~

### Admission

~~~text
one active Admission per single-entry Ticket
~~~

### Idempotency

~~~text
unique caller scope + operation + key
~~~

---

## 34. Protection Triggers

Implement reviewed triggers including:

- GA pool balance;
- GA Reservation reconciliation;
- ReservedInventoryClaim integrity;
- Ticket origin integrity;
- Ticket cardinality;
- QR/Ticket consistency;
- Event snapshot integrity;
- layout-component scope;
- immutable Sale/Issuance history;
- immutable published layout;
- Allocation destination validation;
- transaction currency coherence.

---

## 35. Derived Views

Implement:

~~~text
v_reserved_inventory_current_state
v_ga_inventory_current_summary
~~~

and additional internal views required by approved read/reporting models.

---

## 36. Persistence Test Harness

Complete database integration-test infrastructure capable of:

- independent database creation;
- migration application;
- explicit transactions;
- concurrent requests;
- fixture generation;
- deliberate invalid-state insertion;
- rollback assertions.

---

## 37. M1 Acceptance Tests

### Reserved double claim

Attempt two active claims for one seat.

Expected:

~~~text
database rejects second current claim
~~~

### GA imbalance

Change GA quantity without balancing source/destination.

Expected:

~~~text
transaction rejected at commit
~~~

### Duplicate Admission

Attempt two active Admissions.

Expected:

~~~text
database rejects second active Admission
~~~

### Active QR on void Ticket

Attempt invalid state.

Expected:

~~~text
transaction rejected
~~~

---

## 38. M1 Exit Gate

M1 is complete when:

- empty database builds entirely through migrations;
- schema matches the governing relational specification;
- required constraints/triggers exist;
- invariant-breaking tests fail correctly;
- application code performs no runtime schema creation.

---

## Part V: M2: GO PLATFORM KERNEL, AUTH, IDEMPOTENCY, AUDIT & OUTBOX

## 39. Objective

Build shared backend infrastructure required by every authoritative command.

---

## 40. Transaction Infrastructure

Implement a transaction runner supporting:

- `READ COMMITTED`;
- explicit row locks;
- transaction-scoped queries;
- deadlock detection;
- bounded retry;
- serialization retry where required;
- database-authoritative time.

Provide explicit helpers for:

~~~text
Event lifecycle gate
canonical inventory lock ordering
GA pool locking
Reservation locking
~~~

Do not implement abstractions that hide authoritative SQL ordering.

---

## 41. Authoritative Time

Implement true-current PostgreSQL time for command acceptance guards.

Do not use transaction-start time when lock contention may delay guard evaluation.

---

## 42. Public Identifier Layer

Implement typed external IDs equivalent to:

~~~text
evt_...
res_...
sale_...
tkt_...
inv_...
ga_...
cred_...
scan_...
adm_...
~~~

Resource namespace parsing must be strict.

---

## 43. Business Error Framework

Implement canonical structured errors including:

~~~text
INVENTORY_UNAVAILABLE
INSUFFICIENT_GA_QUANTITY
INVENTORY_NOT_ELIGIBLE_FOR_PARTNER
HOLD_EXPIRED
HOLD_NOT_OWNED
RESERVATION_NOT_MODIFIABLE
CHECKOUT_ALREADY_ACTIVE
CHECKOUT_WINDOW_EXPIRED
PAYMENT_STATUS_UNCERTAIN
RECONCILIATION_EXPIRED
ALREADY_CONFIRMED

EVENT_NOT_ON_SALE
EVENT_PAUSED
EVENT_SALES_CLOSED
EVENT_CANCELLED

PARTNER_DISABLED
PARTNER_EVENT_ACCESS_DISABLED
NOT_AUTHORIZED

TICKET_INVALID
TICKET_VOID
TICKET_ALREADY_ADMITTED
CREDENTIAL_REVOKED

IDEMPOTENCY_CONFLICT
AUTHORITY_TEMPORARILY_UNAVAILABLE
~~~

---

## 44. Idempotency Engine

Implement:

~~~text
principal scope
+ operation type
+ idempotency key
+ normalized request hash
~~~

Same key + same request returns the same logical result.

Same key + changed intent returns:

~~~text
IDEMPOTENCY_CONFLICT
~~~

Concurrent duplicate first requests must not execute the business command twice.

---

## 45. Audit & Transactional Outbox

Create transaction-scoped mechanisms for:

~~~text
business mutation
AuditEvent
OutboxEvent
idempotency completion
~~~

to commit atomically.

No external event publishing happens inside the authoritative transaction.

---

## 46. Authentication Foundation

Implement:

### Partner

~~~text
Authorization: Bearer tkp_<key_id>_<secret>
~~~

with digest verification and separate Partner/Event authorization.

### Human

Supabase Auth/OIDC JWT verification followed by database-backed authorization.

### Cryptographic keyrings

Versioned keyrings for:

- BuyerSelectionSession;
- Reservation continuation;
- QR credentials;
- webhook secret encryption.

---

## 47. M2 Exit Gate

M2 is complete when:

- transaction infrastructure passes retry tests;
- database time guards work;
- idempotency works under concurrency;
- Partner authentication works;
- human authentication works;
- authorization is database-backed;
- AuditEvent writes work;
- OutboxEvent writes work;
- secret material is excluded from logs.

---

## Part VI: M3: VENUE, LAYOUT, EVENT & PARTNER CONFIGURATION

## 48. Objective

Implement the configuration foundation required before inventory can be sold.

---

## 49. Venue & Layout

Implement:

- Venue creation;
- allowed metadata updates;
- draft layout version;
- normalized sections/rows/tables/seats/GA zones;
- floor-plan geometry;
- layout publication;
- layout retirement.

Published physical identity is immutable.

---

## 50. Event

Implement:

- Event creation;
- Event policy configuration;
- EventLayoutSnapshot;
- Event sections;
- reserved inventory materialization;
- GA pool materialization;
- sale window;
- admission window;
- lifecycle commands.

---

## 51. Pricing

Implement:

- EventPriceTier;
- section default pricing;
- reserved-seat override;
- GA pricing;
- active/retired tiers;
- effective pricing resolution.

---

## 52. Partner & Staff Configuration

Implement:

- Partner creation;
- credential issuance/rotation/revocation;
- PartnerEventAccess;
- Partner disable;
- Event staff assignments;
- platform roles.

---

## 53. Explicit Event Lifecycle

Implement:

~~~text
OpenSales
PauseSales
ResumeSales
CloseSales
CancelEvent
CompleteEvent
~~~

Do not use unrestricted generic Event-state PATCH behavior.

---

## 54. M3 Exit Gate

An authorized administrator can:

~~~text
create Venue
 ->
publish layout
 ->
create Event
 ->
materialize inventory
 ->
configure pricing
 ->
grant Partner access
 ->
open Event for sale
~~~

without direct SQL manipulation.

---

## Part VII: M4: INVENTORY, PRICING, BLOCKS, ALLOCATIONS & AVAILABILITY

## 55. Objective

Represent and expose contextual inventory while preserving one authoritative truth and Partner neutrality.

---

## 56. Inventory Operations

Implement:

- reserved current claims;
- GA accounting;
- shared inventory;
- Blocks;
- Allocations;
- Allocation release;
- restriction reclassification;
- safe GA capacity changes.

---

## 57. Availability

Implement caller-contextual availability for:

### Reserved

- shared available seats;
- caller-eligible Allocation seats.

### GA

- shared availability;
- caller-eligible Allocation quantity.

Use opaque offers where acquisition source must remain distinguishable without exposing private Allocation identity.

---

## 58. M4 Exit Gate

- reserved inventory works;
- GA accounting balances;
- Blocks work;
- Allocations work;
- pricing resolves correctly;
- cross-Partner availability privacy passes;
- availability remains non-authoritative.

---

## Part VIII: M5: RESERVATION, CHECKOUT, RETRY, RECONCILIATION & EXPIRY

## 59. Objective

Implement the highest-risk pre-Sale transaction lifecycle.

---

## 60. Reservation Creation

Implement complete all-or-nothing acquisition for:

- reserved;
- GA;
- mixed inventory.

Flow includes:

~~~text
idempotency
Event gate
Partner/access
Allocation locks
inventory locks
authoritative time
validation
price snapshot
Reservation
ReservationItems
inventory mutation
AuditEvent
OutboxEvent
commit
~~~

---

## 61. Reservation Continuation Token

Implement deterministic:

~~~text
rsv1.<key_version>.<reservation_id>.<mac>
~~~

Lost creation responses must be recoverable through idempotent retry.

---

## 62. Reservation Modification

Implement:

- HELD only;
- replacement acquired before original release;
- failure preserves original selection;
- maximum lifetime preserved;
- no timer-reset abuse.

---

## 63. Checkout Protection

Implement:

~~~text
HELD -> COMMITTING
~~~

with one active CheckoutAttempt.

Partner payment begins only after this command succeeds.

---

## 64. Retry & Reconciliation

Implement:

~~~text
COMMITTING -> PAYMENT_RETRY

COMMITTING timeout -> RECONCILING
~~~

Unknown payment remains protected.

---

## 65. Release & Expiry

Implement:

~~~text
HELD -> RELEASED / EXPIRED
PAYMENT_RETRY -> EXPIRED
RECONCILING -> EXPIRED
~~~

with source-aware restoration.

---

## 66. Worker Materialization

Worker must discover overdue candidates but use the normal authoritative transaction/lock hierarchy to materialize state.

Worker delay cannot extend customer rights.

---

## 67. Critical M5 Tests

### Reserved contention

~~~text
100 concurrent requests
same seat
exactly 1 succeeds
~~~

### GA contention

~~~text
10 available
100 quantity-1 requests
exactly 10 units succeed
~~~

### Mixed acquisition

Any component failure means zero acquisition.

### Hold vs Block

Exactly one wins.

### Modification failure

Original Reservation remains.

### Worker delay

Effective expiry still blocks invalid continuation.

---

## 68. M5 Exit Gate

TktSync safely supports:

~~~text
availability
 ->
hold
 ->
modify
 ->
checkout protection
 ->
retry / reconciliation / release / expiry
~~~

under concurrent database testing.

---

## Part IX: M6: CONFIRMATION, SALE, TICKET, QR & NON-PUBLIC ISSUANCE

## 69. Objective

Complete commercial confirmation and Ticket entitlement.

---

## 70. Confirmation Transaction

Implement atomically:

~~~text
Reservation CONFIRMED
inventory SOLD
Sale
SaleItems
TicketEntitlements
QRCredentials
AuditEvents
OutboxEvents
idempotency result
~~~

No phased commit.

---

## 71. QR

Implement:

~~~text
qr1.<key_version>.<credential_id>.<mac>
~~~

with:

- deterministic active credential retrieval;
- no PII;
- database state validation.

---

## 72. Ticket Operations

Implement:

- credential retrieval;
- credential reissue;
- Ticket void;
- explicit inventory re-release;
- Ticket replacement where applicable.

Ticket void does not automatically re-release inventory.

---

## 73. Non-Public Issuance

Implement:

~~~text
NON_PUBLIC Allocation
 ->
NonPublicIssuance
 ->
TicketEntitlement
 ->
QRCredential
~~~

No Sale is created.

---

## 74. M6 Exit Gate

Commercial:

~~~text
AVAILABLE
 ->
HELD
 ->
COMMITTING
 ->
CONFIRMED
 ->
SALE
 ->
TICKET
 ->
QR
~~~

Non-public:

~~~text
ALLOCATION
 ->
ISSUANCE
 ->
TICKET
 ->
QR
~~~

both operate correctly and remain semantically distinct.

---

## Part X: M7: ADMISSION & SCANNER AUTHORITY

## 75. Objective

Implement authoritative online QR validation and duplicate-admission prevention.

---

## 76. ValidateAndAdmit

Verify:

- scanner authorization;
- Event;
- credential MAC;
- credential state;
- Ticket state;
- Event/admission window;
- active Admission;
- idempotency.

Then atomically create:

~~~text
ScanAttempt
Admission
AuditEvent
OutboxEvent
~~~

---

## 77. Duplicate Test

~~~text
100 simultaneous distinct scans
same single-entry Ticket
~~~

Expected:

~~~text
1 ADMITTED
99 ALREADY_ADMITTED
1 active Admission
~~~

---

## 78. Scan Retry

Same idempotency identity after successful scan returns original:

~~~text
ADMITTED
~~~

not duplicate.

---

## 79. Correction & Override

Implement privileged:

- Admission reversal;
- manual override where policy permits;
- mandatory reason;
- audit history.

---

## 80. M7 Exit Gate

Complete lifecycle works:

~~~text
Inventory
 ->
Reservation
 ->
Sale / Issuance
 ->
Ticket
 ->
QR
 ->
Admission
~~~

with duplicate-scan correctness verified.

---

## Part XI: M8: REALTIME, WEBHOOKS & WORKER COMPLETION

## 81. Objective

Activate asynchronous communication from committed OutboxEvents.

---

## 82. Browser Realtime

Implement Go-managed authenticated server-to-client realtime stream.

Preferred MVP transport:

~~~text
SSE-compatible stream
~~~

Deliver sanitized invalidation events only.

---

## 83. Partner Webhooks

Implement:

- Partner webhook endpoints;
- subscriptions;
- encrypted signing secrets;
- delivery records;
- retries;
- dead-letter state;
- manual replay where supported.

---

## 84. Webhook Security

Implement:

~~~text
HMAC-SHA-256
HTTPS only
no redirect following
SSRF prevention
private/link-local/metadata blocking
DNS validation
bounded response handling
secret rotation overlap
~~~

---

## 85. Async Semantics

Browser realtime:

~~~text
best effort
non-gap-free
non-authoritative
~~~

Webhooks:

~~~text
at least once
duplicate possible
no strict global ordering
~~~

Authoritative API/database state always wins.

---

## 86. M8 Exit Gate

- outbox survives dispatcher restart;
- realtime outage does not affect transactions;
- reconnect refresh works;
- webhook retries work;
- duplicate webhook delivery is safe;
- dead-letter handling works;
- cross-Partner privacy passes.

---

## Part XII: M9: COMPLETE REACT PRODUCT SURFACES

## 87. Objective

Complete all three React/Vite user-facing applications.

---

## 88. Admin Web

Complete:

- Venue/Layout;
- Event;
- pricing;
- inventory;
- Blocks;
- Allocations;
- Partner configuration;
- Event lifecycle;
- Ticket operations;
- admission operations.

---

## 89. White-Label Selector

Complete:

~~~text
secure capability bootstrap
 ->
layout
 ->
availability
 ->
seat / GA selection
 ->
Reservation
 ->
modification
 ->
timer
 ->
secure Partner return
~~~

No payment or Sale confirmation occurs in Selector.

---

## 90. Scanner

Complete:

- camera scanning;
- authoritative result;
- duplicate state;
- invalid/revoked/void state;
- authority unavailable state;
- Event cancellation behavior.

---

## 91. M9 Exit Gate

All normal MVP user workflows operate without manual database intervention.

---

## Part XIII: M10: REPORTING, AUDIT OPERATIONS & ACCREDITATION

## 92. Objective

Complete derived operational visibility.

---

## 93. Reporting

Implement Partner/Admin reporting while preserving:

~~~text
AVAILABLE
RESERVED
BLOCKED
ALLOCATED
SOLD
ISSUED
VOIDED
ACTIVE ADMISSION
REVERSED ADMISSION
REJECTED SCAN
~~~

---

## 94. Audit Explorer

Provide authorized inspection of material AuditEvents.

Audit remains append-only.

---

## 95. Accreditation

Implement derived accreditation export from:

- Event;
- Allocation;
- NonPublicIssuance;
- Ticket;
- optional attendee/accreditation information;
- Admission where applicable.

Exports are snapshots only.

---

## 96. Observability

Add metrics/alerts covering:

- request latency/error;
- lock contention;
- hold conflict;
- confirmation;
- reconciliation;
- worker lag;
- outbox lag;
- webhook failures;
- scan outcomes;
- authentication anomalies.

---

## 97. M10 Exit Gate

Required reports, audit workflows, exports, metrics, and alerts operate without becoming alternate sources of truth.

---

## Part XIV: M11: CONCURRENCY, SECURITY, PARTNER CERTIFICATION & MVP RELEASE

## 98. Objective

Prove the complete system under concurrency, failure, security abuse, and realistic Partner integration.

---

## 99. Concurrency Certification

Test:

~~~text
hold vs hold
hold vs block
GA contention
confirm vs expiry
confirm vs cancellation
scan vs scan
QR reissue vs scan
Ticket void vs scan
~~~

---

## 100. Idempotency Certification

For every externally retriable mutation:

- same key/same request;
- same key/different request;
- concurrent first request;
- process failure before commit;
- response loss after commit;
- DB deadlock/retry;
- network timeout.

No duplicate logical effects.

---

## 101. Failure Injection

Test:

- PostgreSQL unavailable;
- worker unavailable;
- realtime unavailable;
- dispatcher unavailable;
- webhook unavailable;
- API crash after commit.

Failure behavior must match governing specifications.

---

## 102. Security Review

Verify:

- cross-Partner isolation;
- Event authorization;
- capability scope;
- scanner permissions;
- credential revocation;
- Partner disable semantics;
- secret leakage;
- CORS;
- CSP;
- QR forgery rejection;
- webhook signature;
- webhook replay tolerance;
- SSRF;
- key rotation.

---

## 103. Load Testing

Measure:

- availability;
- hold contention;
- GA concurrency;
- confirmation;
- Admission throughput;
- worker backlog recovery;
- realtime connections;
- webhook throughput.

Correctness tests must remain passing during performance optimization.

---

## 104. Partner Certification

Validate required Partner workflows:

~~~text
authentication
Event discovery
availability
hold
expiry
checkout protection
payment failure
confirmation
lost-response recovery
release
Ticket retrieval
credential retrieval
webhook validation
duplicate webhook
Partner disable
Event cancellation
~~~

MVP should validate the required two-to-three Partner integrations.

---

## 105. End-to-End Scenarios

### Reserved

~~~text
availability
 ->
hold
 ->
checkout
 ->
Partner payment
 ->
confirm
 ->
Ticket
 ->
QR
 ->
Admission
~~~

### GA

Quantity acquisition produces independent Tickets.

### Mixed

Reserved + GA remains one atomic Reservation.

### Channel Allocation

Source restoration remains correct.

### Non-public

Allocation -> Issuance -> Ticket with no Sale.

### Payment uncertainty

COMMITTING -> RECONCILING -> confirmed or safely expired.

### Cancellation

No new Sale after cancellation wins authoritative ordering.

---

## 106. MVP Release Gate

The MVP is releasable only when:

### Inventory

- no reserved oversell under concurrency;
- GA never negative;
- mixed atomicity works;
- source restoration works.

### Reservation

- deadlines authoritative;
- checkout protection correct;
- reconciliation bounded;
- worker delay does not extend rights.

### Commercial

- confirmation exactly once;
- Ticket cardinality correct;
- SOLD/ISSUED distinct.

### Admission

- duplicate scan protection correct;
- retry idempotency correct.

### Security

- isolation verified;
- capabilities scoped;
- secrets protected;
- webhook security verified;
- key rotation verified.

### Async

- outbox durable;
- realtime remains non-authoritative;
- webhook duplication safe.

### Operations

- migrations;
- observability;
- alerts;
- deployment;
- backup/recovery procedures.

### Product

- Admin complete;
- Selector complete;
- Scanner complete;
- Partner integrations complete;
- accreditation export complete.

---

## Part XV: PARALLELIZATION

## 107. Safe Parallel Work

After the relevant foundations are stable:

### Core

~~~text
Inventory
Reservation
Ticketing
Admission
~~~

### Frontend

~~~text
Admin UI
Selector UI
Scanner UI
~~~

against generated/mocked contracts.

### Infrastructure

~~~text
CI/CD
observability
deployment
realtime
webhook infrastructure
~~~

### Developer Experience

~~~text
OpenAPI
generated clients
Partner examples
Stripe-like Partner documentation surface
webhook verification examples
~~~

---

## 108. Unsafe Parallelization

Do not independently implement competing semantics for:

~~~text
Reservation + inventory locking
Confirmation + Ticket creation
GA + Allocation accounting
QR + Ticket void
Admission + ScanAttempt
Cancellation + confirmation
~~~

These share authoritative consistency boundaries.

---

## Part XVI: DEFINITIONS OF DONE

## 109. Backend Command

A command is complete only when it has:

1. authorization;
2. validation;
3. idempotency where required;
4. canonical locking;
5. authoritative time guard where required;
6. database constraints;
7. domain mutation;
8. AuditEvent;
9. OutboxEvent;
10. stable business errors;
11. OpenAPI;
12. integration tests;
13. concurrency tests where relevant;
14. failure/retry tests;
15. secret-safe logs.

---

## 110. API Operation

Requires:

- request schema;
- response schema;
- public identifiers;
- auth requirements;
- idempotency behavior;
- error behavior;
- OpenAPI;
- generated client;
- integration tests.

---

## 111. Frontend Workflow

Requires:

- loading;
- empty state;
- conflict state;
- network failure;
- authority unavailable where applicable;
- expiry handling where applicable;
- responsive UX;
- contract-generated API usage;
- no secret leakage;
- authoritative resync after realtime interruption.

---

## Part XVII: REVIEW CHECKPOINTS

## 112. Checkpoint After M2

Review:

- schema;
- transactions;
- idempotency;
- authentication;
- authorization;
- audit;
- outbox.

---

## 113. Checkpoint After M5

Review complete Reservation lifecycle.

This is the primary inventory-correctness review.

Do not rush into Sale confirmation if this milestone is not proven.

---

## 114. Checkpoint After M7

Review:

- confirmation;
- Ticket lifecycle;
- QR;
- Admission;
- concurrency.

---

## 115. Checkpoint After M8

Confirm asynchronous delivery has not become a second authority.

---

## 116. Final M11 Review

Perform complete traceability review against every governing TktSync document.

---

## Part XVIII: TRACEABILITY

## 117. Technical Brief Coverage

| Requirement | Milestone |
|---|---|
| Project scaffold | M0 |
| PostgreSQL/Supabase | M0/M1 |
| Venue/Event | M3 |
| Floor-plan builder | M3/M9 |
| GA | M4 |
| Reserved seating | M4 |
| Mixed inventory | M5 |
| Partner integrations | M3/M11 |
| Realtime locks/timers | M5/M8 |
| White-label selector | M4/M5/M9 |
| Blocks/Allocations | M4 |
| QR | M6 |
| Admission | M7 |
| Dashboard | M3/M9 |
| Audit | M2 onward |
| Reporting | M10 |
| Accreditation | M10 |

---

## 118. Governing Principle

> **TktSync is implemented as a sequence of proven correctness boundaries. Scaffold first, establish the database and platform kernel second, then build inventory ownership, customer transaction state, commercial confirmation, admission, asynchronous delivery, and product interfaces on top of those verified foundations.**

---

## 119. Hosted Ticket QR Gap Closure

TktSync now generates an actual SVG QR image from the complete authoritative `qr1...` credential and exposes two narrow delivery surfaces:

- authenticated Partner retrieval at `GET /api/v1/partner/tickets/{ticket_id}/qr`, using the existing commercial Ticket ownership boundary;
- buyer-usable retrieval at `GET /api/v1/ticket-qr/{opaque-capability}`, using an authenticated-encrypted, unguessable ticket-level presentation capability.

The credential response includes `qr_url`. The capability contains no raw credential, public Ticket ID, Partner credential, or Reservation token. It authorizes QR presentation only, is redacted from request route logging, and resolves dynamically to the current active credential.

Credential reissue invalidates the previous `qr1...` credential while the same hosted URL renders the replacement. Ticket void or any no-active-credential state returns a non-success business response and never serves stale QR content. Both image routes return generated `image/svg+xml` with `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

Historical note: final submission hardening changed the current governing design to a credential-bound hosted capability. Under the released contract, reissue invalidates both the old `qr1...` credential and old hosted URL; authenticated retrieval returns the replacement pair. See the Security and API Contract documents for current behavior.

This closes QR image generation and hosting without adding a hosted ticket page: Partners retain ticket design, branding, payment, and customer-delivery responsibility.

---

**End of Document**

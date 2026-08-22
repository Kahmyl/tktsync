# TktSync System Architecture & Transactional Design Specification

**Document status:** Governing System Architecture & Transactional Design  
**Applies to:** TktSync MVP and compatible partner integrations  
**Version:** 1.0  
**Date:** 20 August 2026  
**Classification:** Confidential  
**Normative parents:**  
1. TktSync Platform Process & Policy Standard v1.0  
2. TktSync Logical Domain Specification v1.0  
**Product basis:** TktSync Technical Brief (2026)

---

## 1. Purpose

This document defines the governing system architecture, transactional consistency model, concurrency strategy, failure behavior, deployment topology, background processing model, realtime model, security boundaries, and command-level transaction design for TktSync.

It translates the approved platform policy and logical domain model into an enforceable implementation architecture.

This specification is intended to prevent implementation drift. Lower-level database schemas, API contracts, service code, workers, realtime consumers, dashboards, scanners, and deployment configuration MUST preserve the architecture and transactional semantics defined here.

Where an implementation choice conflicts with a governing platform policy or logical-domain invariant, the higher-level policy or domain specification takes precedence.

---

## 2. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

- **MUST / MUST NOT** define mandatory architecture or transactional behavior.
- **SHOULD / SHOULD NOT** define expected architecture unless an explicitly reviewed exception exists.
- **MAY** defines permitted behavior.

An implementation detail not explicitly prescribed by this document MAY vary only when it does not alter domain semantics, consistency guarantees, authority boundaries, failure behavior, or security properties.

---

## 3. Authority and Precedence

The following precedence governs implementation decisions:

1. **TktSync Platform Process & Policy Standard v1.0**
2. **TktSync Logical Domain Specification v1.0**
3. **This System Architecture & Transactional Design Specification**
4. Database schema specification
5. API and partner-integration specification
6. Realtime/event contract
7. Security/auth implementation specification
8. Application code and deployment configuration

This document MUST NOT reinterpret `SOLD`, `ISSUED`, `HELD`, `COMMITTING`, `RECONCILING`, `ACTIVE`, `VOIDED`, `ADMITTED`, or any other canonical domain term.

---

## 4. Architecture Objectives

The architecture MUST optimize for the following properties, in this order:

1. **No overselling.**
2. **No contradictory inventory ownership.**
3. **Protection of legitimate buyer rights already accepted by TktSync.**
4. **Exactly-once logical business outcomes under retried requests.**
5. **Deterministic behavior under concurrency.**
6. **Failure-closed behavior where authoritative state cannot be established.**
7. **Accurate immutable business history.**
8. **Clear partner, event, buyer, and scanner isolation.**
9. **Operational simplicity appropriate to the MVP.**
10. **Ability to scale without changing domain semantics.**

Performance optimizations MUST NOT weaken any item above.

---

## 5. Architecture Non-Goals

The MVP architecture does not require:

- microservices;
- distributed transactions;
- distributed locks;
- Redis-based ownership;
- an external message broker;
- native mobile applications;
- payment processing;
- dynamic pricing;
- offline duplicate-safe scanning;
- event sourcing as the primary persistence model;
- multi-region active-active inventory writes;
- arbitrary workflow orchestration.

These capabilities MAY be introduced later only through an architecture revision that preserves the governing invariants.

---

## 6. Governing Architectural Decision

TktSync SHALL use a **modular monolith with one authoritative PostgreSQL transactional database** for the MVP.

The modular monolith is a single authoritative application boundary containing explicit domain modules. It is not an unstructured monolith.

The primary reason for this architecture is that the most important TktSync operations span multiple logical aggregates but require one atomic business outcome. These operations include:

- multi-seat and mixed-inventory hold creation;
- hold modification;
- confirmation;
- release/expiry;
- block/allocation mutation;
- non-public issuance;
- ticket void and inventory re-release;
- admission validation.

Keeping these modules in one application and one transactional database permits these outcomes to be enforced by one ACID transaction rather than distributed sagas.

---

## 7. High-Level System Topology

~~~text
                           ┌──────────────────────┐
                           │ Ticketing Partners   │
                           └──────────┬───────────┘
                                      │
                               Partner API
                                      │
                                      ▼
┌────────────────┐       ┌────────────────────────────┐
│ Admin Web      │──────▶│                            │
└────────────────┘       │      TktSync Core API      │
                         │                            │
┌────────────────┐       │  Event & Inventory        │
│ White-label    │──────▶│  Reservation & Checkout   │
│ Buyer Selector │       │  Restrictions/Allocations │
└────────────────┘       │  Ticketing/Entitlements   │
                         │  Admission                 │
┌────────────────┐       │  Partner/Auth              │
│ Scanner Web    │──────▶│  Audit/Outbox              │
└────────────────┘       │  Reporting/Projections     │
                         └─────────────┬──────────────┘
                                       │
                                       ▼
                              ┌──────────────────┐
                              │ PostgreSQL       │
                              │ Source of Truth  │
                              └────────┬─────────┘
                                       │
                                  durable facts
                                       │
                         ┌─────────────┴─────────────┐
                         ▼                           ▼
                ┌──────────────────┐       ┌──────────────────┐
                │ Background Worker│       │ Outbox Dispatcher│
                │ expiry/reconcile │       │ realtime/events  │
                └──────────────────┘       └────────┬─────────┘
                                                    │
                                                    ▼
                                           ┌──────────────────┐
                                           │ Realtime Channel │
                                           └──────────────────┘
~~~

---

## 8. Deployment Units

The MVP SHALL use the following logical deployment units.

### 8.1 Core API

A stateless web service responsible for:

- authenticated partner commands;
- admin commands;
- white-label buyer commands;
- scanner commands;
- authoritative reads required for transactions;
- caller-contextual availability;
- transaction coordination;
- authorization;
- idempotency enforcement;
- audit and outbox creation.

Multiple API instances MAY run concurrently.

No correctness property may depend on process-local memory.

### 8.2 Background Worker

A separate process using the same application/domain codebase.

Responsibilities include:

- materializing overdue Reservation transitions;
- reconciliation timeout processing;
- retry timeout processing;
- outbox dispatch support where separated operationally;
- non-authoritative projection maintenance where required;
- scheduled maintenance jobs.

Worker failure MUST affect cleanup freshness, not authoritative eligibility.

### 8.3 Outbox Dispatcher

A worker process or worker responsibility that reads committed outbox records and publishes post-commit facts.

It MUST NOT mutate authoritative business state merely because a realtime publication failed.

### 8.4 Admin Web Application

A web application for event administrators and platform administrators.

It operates only through authorized API contracts.

It MUST NOT connect directly to privileged database mutation paths.

### 8.5 White-Label Buyer Selection Application

A mobile-first web application used for partners without native seat-selection UI.

It uses narrowly scoped buyer-selection capability credentials.

It MAY:

- read contextual availability;
- create its own hold;
- modify its own `HELD` Reservation;
- release its own `HELD` Reservation.

It MUST NOT:

- confirm a Sale;
- process payment;
- access partner-secret APIs;
- administer inventory.

### 8.6 Scanner Web Application

A mobile web application for event admission.

It submits credentials to the authoritative admission API.

MVP duplicate-prevention guarantees require online access to authoritative state.

---

## 9. Reference Infrastructure Baseline

The architecture SHALL use the following baseline unless formally revised.

| Layer | Baseline |
|---|---|
| Transactional database | PostgreSQL, managed through Supabase |
| Realtime transport | Supabase Realtime or equivalent transport fed by committed outbox facts |
| Core backend | Modular server application with explicit transaction support |
| Worker runtime | Same backend codebase, separate worker process |
| Web applications | React/Next.js-class web applications |
| Frontend hosting | Vercel-class hosting |
| API/worker hosting | Render-class persistent runtime |
| QR generation | Standard QR library |
| Floor-plan engine | Third-party seat-map/floor-plan library |
| Cache | None required initially |
| Distributed lock service | None |
| External message broker | None required initially |

The framework or SQL abstraction layer MUST support:

- explicit transactions;
- `SELECT ... FOR UPDATE`;
- `FOR KEY SHARE` or equivalent row-lock modes;
- deterministic lock acquisition;
- constraint-error handling;
- raw SQL where necessary;
- reliable access to PostgreSQL server time.

An ORM that obscures or prevents these capabilities MUST NOT be used for authoritative transaction code.

---

## 10. PostgreSQL as the Sole Transactional Authority

PostgreSQL SHALL authoritatively determine:

- Event lifecycle state;
- Partner and PartnerEventAccess state;
- Reservation lifecycle;
- reserved inventory disposition;
- GA pool accounting;
- blocks and allocations;
- Sale creation;
- non-public issuance;
- TicketEntitlement state;
- QRCredential state;
- Admission state;
- idempotency result;
- audit facts;
- outbox facts.

The following MUST NOT be authoritative for ownership or irreversible state:

- Redis;
- application memory;
- WebSocket messages;
- browser state;
- seat-map state;
- availability caches;
- worker queues;
- export files;
- dashboards;
- read replicas;
- analytics projections.

---

## 11. No Distributed Lock Dependency

TktSync MUST NOT use Redis locks, in-memory mutexes, or message-queue serialization as the primary guarantee against overselling.

Any optional future Redis deployment MAY be used for:

- rate limiting;
- ephemeral read caching;
- non-authoritative session acceleration.

It MUST NOT be required to decide whether inventory is owned, held, sold, issued, or admitted.

---

## 12. Transaction Isolation Baseline

The default transaction isolation level SHALL be PostgreSQL **READ COMMITTED**, combined with:

- explicit row-level locks;
- canonical lock ordering;
- conditional state guards;
- uniqueness/check constraints;
- bounded retry for deadlock or serialization failures.

`SERIALIZABLE` MAY be used for an isolated operation only when its retry behavior is explicitly handled and it provides a concrete advantage.

The architecture MUST NOT rely on isolation level alone to understand domain races.

---

## 13. Global Lock Ordering

All authoritative transactions MUST follow a consistent lock hierarchy.

Canonical order:

0. **Request-scoped idempotency claim**, where required
1. **Event lifecycle gate**
2. **Partner / PartnerEventAccess**, where the command requires current acquisition authority
3. **Existing transaction aggregate**, where applicable
   - Reservation
   - TicketEntitlement
   - InventoryRestriction
4. **Relevant restriction/allocation aggregates**
5. **Reserved inventory units**, ordered by stable identifier
6. **GA inventory pools**, ordered by stable identifier
7. **Dependent records and append-only facts**
   - ReservationItems
   - CheckoutAttempts
   - Sale/SaleItems
   - TicketEntitlements
   - QRCredentials
   - Admissions
   - AuditEvents
   - OutboxEvents
8. **Idempotency completion/result update**

The idempotency claim is a request-deduplication gate rather than a domain ownership lock. Its only purpose before domain locking is to ensure that two concurrent requests with the same operation identity cannot both execute the business command. The successful result is finalized only inside the same commit as the business mutation.

A command MUST NOT invent a different domain lock order because client input arrived in a different sequence.

---

## 14. Event Lifecycle Gate

High-volume operations that require Event state to remain stable during a transaction SHALL acquire a lightweight shared row lock on the Event.

Recommended PostgreSQL mode:

`FOR KEY SHARE`

Lifecycle mutation commands such as:

- `OpenSales`
- `PauseSales`
- `ResumeSales`
- `CloseSales`
- `CancelEvent`
- `CompleteEvent`

SHALL acquire an incompatible exclusive row lock such as:

`FOR UPDATE`

This produces authoritative ordering between Event lifecycle changes and in-flight commands.

### 14.1 Confirmation vs Cancellation

If confirmation acquires and passes the Event guard before cancellation obtains its exclusive gate:

- confirmation may commit;
- Sale/Ticket history becomes authoritative;
- later cancellation changes admission eligibility.

If cancellation commits first:

- later confirmation observes `CANCELLED`;
- new commercial Sale creation is rejected;
- external payment remediation remains the Partner's responsibility.

### 14.2 Hold vs Pause/Close

If a new hold authoritatively passes the Event gate while `ON_SALE` before pause/close commits, that hold may finish.

If pause/close commits first, later hold acquisition MUST fail.

---

## 15. Partner and PartnerEventAccess Concurrency Gate

New inventory acquisition SHALL validate current Partner and PartnerEventAccess authority while holding suitable shared locks.

Operational disable commands SHALL use incompatible write locks.

Once Partner disable commits:

- new holds MUST fail;
- inventory-expanding modifications MUST fail.

Pre-existing Reservations MAY continue according to their already-authorized windows under the logical-domain graceful-disable rules.

Credential revocation is separate from operational disable.

---

## 16. Deterministic Inventory Locking

### 16.1 Reserved Inventory

All reserved inventory requested by one transaction MUST be sorted by stable database/domain identifier before row locks are taken.

### 16.2 GA Pools

All GA pools requested by one transaction MUST be sorted by stable identifier before row locks are taken.

### 16.3 Mixed Requests

For mixed reserved-seat and GA requests:

1. lock relevant allocations/restrictions in stable identifier order;
2. lock reserved inventory units in stable identifier order;
3. lock GA pools in stable identifier order.

This ordering MUST be used consistently by hold, modification, release, expiry, confirmation, and administrative inventory operations.

---

## 17. Deadlock Handling

Canonical lock ordering minimizes but does not mathematically eliminate all database deadlocks.

The transaction infrastructure SHALL:

- detect PostgreSQL deadlock/serialization errors;
- retry the complete transaction a small bounded number of times;
- preserve the same idempotency identity across retries;
- apply short jittered backoff;
- return a temporary conflict/unavailable outcome if the bounded retry budget is exhausted.

A deadlock retry MUST NOT create a second business operation.

---

## 18. Server-Authoritative Time

PostgreSQL server time SHALL govern all authoritative deadlines.

This includes:

- hold expiry;
- checkout protection;
- payment retry;
- reconciliation;
- sale windows;
- admission windows;
- scan timestamps;
- audit timestamps.

### 18.1 Guard-Time Rule

The time used to decide whether a command is accepted MUST be obtained **after the command has acquired all locks needed to evaluate its authoritative state, ownership, and deadline guards**.

PostgreSQL transaction-start time MUST NOT be used for this purpose when the transaction may have waited on locks.

In PostgreSQL, implementation SHOULD use `clock_timestamp()` or an equivalent true-current-time source at guard evaluation.

Using `now()` / `CURRENT_TIMESTAMP` as an acceptance clock is unsafe because PostgreSQL fixes those values at transaction start.

Each command SHOULD capture one `accepted_at` instant after the final required guard locks are acquired and use that same instant for the command's deadline decision, audit correlation, and accepted-before-deadline semantics. Preliminary early checks MAY reject obviously expired requests sooner, but only the final locked guard establishes acceptance.

### 18.2 Accepted-Before-Deadline

A command is considered authoritatively accepted only after:

- authentication/authorization succeeds;
- required authoritative locks have been obtained;
- applicable time/state guards pass.

Arrival at the HTTP edge or load balancer does not constitute acceptance.

Once accepted under the guard, subsequent internal execution time MUST NOT retroactively invalidate the operation.

---

## 19. Logical Expiry vs Materialized Expiry

A persisted status is not sufficient to determine current Reservation rights.

Example:

~~~text
status = HELD
hold_expires_at = 19:00:00
current authoritative time = 19:00:05
~~~

The Reservation is effectively expired even if a cleanup worker has not yet updated the row.

Every command interacting with a Reservation MUST evaluate effective state before proceeding.

A delayed worker MUST NOT extend a hold.

---

## 20. Background Expiry/Reconciliation Processing

Background processing materializes time-derived state for cleanup and observability.

It is not the authority that makes deadlines real.

### 20.1 Candidate Discovery

Workers MAY query indexed deadlines to discover candidate Reservation IDs.

Candidate discovery SHOULD avoid retaining Reservation row locks while attempting to acquire the Event lifecycle gate.

### 20.2 Per-Reservation Processing

Each materialization transaction SHALL use normal canonical lock ordering:

1. acquire Event lifecycle gate;
2. lock Reservation;
3. evaluate effective state using authoritative time;
4. lock relevant source Allocations;
5. lock inventory;
6. apply transition;
7. write audit/outbox facts;
8. commit.

### 20.3 Worker Duplication

Multiple workers MAY discover the same candidate.

Only one committed state transition may succeed.

Other workers re-read the locked Reservation and no-op once the transition is already materialized.

### 20.4 Worker Failure

If all expiry workers stop:

- expired Reservations MUST still fail command guards;
- stale held inventory MUST not be acquirable until its authoritative source transition can be safely materialized;
- no operation may assume the absence of worker execution means rights remain valid.

---

## 21. Idempotency Architecture

All externally retriable state-changing commands SHALL use durable idempotency.

Logical uniqueness scope SHALL include at least:

- caller/Partner security scope;
- operation type;
- idempotency key.

### 21.1 Idempotency Record

The durable idempotency record SHALL logically contain:

- security/caller scope;
- operation type;
- idempotency key;
- canonical request fingerprint/hash;
- execution state;
- logical result type;
- affected business-object reference(s);
- replayable result metadata;
- creation timestamp;
- completion timestamp.

### 21.2 Same Key, Same Request

A retry of the same logical request MUST return the same logical result.

### 21.3 Same Key, Different Request

The same idempotency identity with materially different normalized request content MUST return:

`IDEMPOTENCY_CONFLICT`

### 21.4 Atomicity

The successful idempotency result MUST commit in the same database transaction as the business mutation.

The system MUST NOT permit:

- business mutation committed but successful idempotency result absent;
- successful idempotency result committed but business mutation rolled back.

### 21.5 Concurrent First Requests

If two identical requests with the same idempotency identity arrive concurrently, one execution owns the operation. The other MUST wait, observe, or replay the same logical result rather than run a second mutation.

---

## 22. Transactional Outbox

All material domain facts required for realtime, projections, notifications, or downstream processing SHALL be written to a durable outbox in the same transaction as the authoritative mutation.

Example:

~~~text
BEGIN

change Reservation
change inventory
create Sale/Ticket
write AuditEvent
write OutboxEvent

COMMIT
~~~

A process crash after commit therefore cannot erase the fact that downstream publication is still required.

---

## 23. Outbox Dispatch Semantics

Outbox delivery SHALL be **at least once**.

Consumers MUST tolerate duplicate delivery.

Outbox rows SHOULD contain:

- event/fact identity;
- fact type;
- aggregate/business reference;
- Event reference;
- payload or projection key;
- committed timestamp;
- delivery state;
- retry metadata.

The dispatcher MAY use `FOR UPDATE SKIP LOCKED` because outbox rows are not part of the domain lock hierarchy.

Publication failure MUST NOT roll back or alter already committed business state.

---

## 24. Realtime Architecture

Realtime is a freshness mechanism, not a transactional authority.

### 24.1 Realtime Source

Realtime publication SHOULD originate from committed outbox facts.

Direct database change feeds MAY be used only if they preserve the same post-commit semantics and do not expose private internal state.

### 24.2 Payload Strategy

Realtime inventory messages SHOULD favor change notification over globally authoritative availability assertions.

Representative payload:

~~~json
{
  "type": "inventory.changed",
  "eventId": "evt_...",
  "affectedInventory": ["inv_...", "inv_..."],
  "revision": 4821
}
~~~

Clients then refresh their caller-contextual availability.

### 24.3 Contextual Availability

Because channel Allocations can make availability Partner-specific, the realtime layer MUST NOT broadcast one global statement such as:

`A12 is available to everyone`

unless that statement is actually true for every audience.

### 24.4 Reconnect

Clients reconnecting after realtime interruption MUST re-fetch current authoritative/contextual state.

Realtime message sequence alone MUST NOT be used to reconstruct irreversible ownership.

---

## 25. Availability Read Model

Availability is a derived projection.

For the MVP, authoritative PostgreSQL reads are sufficient and no external cache is required.

The availability service MAY compute:

- shared available reserved units;
- caller-eligible allocated reserved units;
- shared GA quantity;
- caller-eligible allocated GA quantity;
- event-controlled display pricing;
- freshness/revision metadata.

Availability MUST NOT expose another Partner's private Reservation/order/payment metadata.

A successful availability response NEVER guarantees acquisition.

---

## 26. Caching Policy

No cache is required for the initial MVP.

If caching is introduced:

- cache is read-only with respect to ownership;
- hold creation always revalidates against PostgreSQL;
- cache misses MUST fall back to authoritative reads;
- stale cache may cause a hold conflict but MUST NOT cause overselling;
- cache outage MUST degrade performance, not correctness.

---

# PART II — COMMAND TRANSACTION DESIGNS

## 27. Venue, Layout, Event, and Pricing Configuration Transactions

### 27.1 Venue Layout Publication

Publishing a VenueLayoutVersion SHALL be one administrative transaction that:

1. validates actor authority;
2. locks the Venue aggregate;
3. validates the draft layout is structurally complete;
4. freezes stable physical identities;
5. transitions the layout version `DRAFT -> PUBLISHED`;
6. writes audit/outbox facts;
7. commits.

A published layout version MUST NOT be materially edited in place. Material changes create a new version.

### 27.2 Event Creation and Layout Snapshot

Creating/configuring Event inventory while the Event is `DRAFT` SHALL:

1. lock Event `FOR UPDATE`;
2. bind or copy the selected published VenueLayoutVersion into an EventLayoutSnapshot;
3. materialize event-specific reserved inventory identities and GA pools;
4. create event-specific pricing assignments;
5. validate that generated inventory matches the snapshot;
6. audit the configuration boundary;
7. commit.

Once protected business history exists, the Event layout snapshot and physical inventory identity MUST NOT be silently regenerated.

### 27.3 `OpenSales`

`OpenSales` SHALL:

1. claim idempotency where the command is externally retriable;
2. lock Event `FOR UPDATE`;
3. validate Event = `DRAFT`;
4. validate a finalized EventLayoutSnapshot exists;
5. validate reserved and/or GA inventory integrity;
6. validate required price assignments;
7. validate sale-window configuration;
8. validate no configuration error would make inventory ambiguous;
9. transition Event -> `ON_SALE`;
10. write audit/outbox/idempotency result;
11. commit.

No inventory row needs to be mass-updated merely to become purchasable; Event state controls acquisition eligibility.

### 27.4 Event Pricing Mutation

Pricing changes are allowed only where governing Event policy permits them.

A pricing mutation SHALL acquire Event `FOR UPDATE`.

This exclusive lifecycle/configuration gate orders pricing changes against new holds, whose shared Event gate prevents them from snapshotting pricing midway through an administrative update.

Existing ReservationItem commercial snapshots are immutable and MUST NOT be repriced.

After the pricing transaction commits, future holds snapshot the new authoritative terms.

### 27.5 Draft Hard Deletion

Venue/Event configuration may be physically deleted only when the logical-domain hard-deletion rules permit it: the object is still draft/unprotected and no business history requiring explanation exists.

After protected business history exists, normal deletion becomes archival/retention behavior rather than destructive erasure.

---

## 28. `CreateReservationHold`

### 27.1 Purpose

Atomically acquire the complete requested inventory set and create one `HELD` Reservation.

### 27.2 Inputs

Logical inputs include:

- Event;
- owning Partner;
- buyer/session/order reference;
- requested inventory selections;
- requested quantities;
- explicit source Allocation where applicable or eligibility context;
- idempotency identity.

### 27.3 Transaction Sequence

~~~text
BEGIN

1. establish/claim idempotency execution
2. lock Event lifecycle gate (shared)
3. verify Event = ON_SALE
4. lock Partner and PartnerEventAccess as required
5. verify acquisition authority
6. lock relevant source Allocations in stable order
7. lock requested reserved units in stable order
8. lock requested GA pools in stable order
9. evaluate eligibility and source disposition for every item
10. verify complete request can be acquired
11. snapshot event-controlled commercial terms
12. create Reservation(HELD)
13. create complete ReservationItem set
14. move reserved units to RESERVED
15. move GA quantities into active_reserved
16. record acquisition source for every item
17. create AuditEvent(s)
18. create OutboxEvent(s)
19. persist successful idempotency result

COMMIT
~~~

### 27.4 All-or-Nothing Requirement

If any selected seat or requested GA quantity cannot be acquired:

- no Reservation is created;
- no requested seat is held;
- no GA quantity is consumed;
- no partial success is returned.

### 27.5 Shared vs Allocation Source

For shared inventory:

`AVAILABLE -> RESERVED`

For channel-allocated inventory:

`ALLOCATED -> RESERVED`

ReservationItem MUST preserve source identity so later release can restore the correct destination.

### 27.6 GA Validation

For a GA source:

`eligible_available_quantity >= requested_quantity`

MUST be true while the pool/allocation is locked.

### 27.7 Concurrency Result

Two transactions cannot both acquire the same reserved unit.

Concurrent GA transactions cannot consume more than eligible capacity.

### 27.8 Error Semantics

Expected business outcomes include:

- `EVENT_NOT_ON_SALE`
- `PARTNER_DISABLED`
- `PARTNER_EVENT_ACCESS_DISABLED`
- `INVENTORY_UNAVAILABLE`
- `INVENTORY_NOT_ELIGIBLE_FOR_PARTNER`
- `INSUFFICIENT_GA_QUANTITY`
- `IDEMPOTENCY_CONFLICT`

---

## 29. `ModifyReservationHold`

### 28.1 Eligibility

Allowed only when effective Reservation state is `HELD`.

When Event is `PAUSED` or `SALES_CLOSED`, only non-expanding changes that release inventory are permitted.

### 28.2 Transaction Sequence

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate
3. lock Reservation
4. evaluate effective expiry
5. validate owner/scope
6. compute old selection vs requested new selection
7. lock relevant source Allocations
8. lock all old/new affected reserved units in canonical order
9. lock affected GA pools in canonical order
10. validate every newly requested acquisition
11. acquire all replacement/additional inventory
12. only after successful acquisition, release removed inventory
13. update ReservationItems
14. preserve original maximum-lifetime boundary
15. snapshot terms only for newly acquired items
16. audit/outbox/idempotency
COMMIT
~~~

### 28.3 Failure Preservation

If replacement inventory cannot be fully acquired, the existing valid Reservation composition MUST remain unchanged.

### 28.4 No Timer Reset

Modification MUST NOT reset `max_lifetime_at`.

A normal hold deadline MAY be adjusted only within explicit bounded policy and MUST never exceed maximum lifetime.

---

## 30. `BeginCheckout`

### 29.1 Purpose

Protect a frozen Reservation before a potentially chargeable payment attempt.

### 29.2 Transaction Sequence

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate
3. lock Reservation
4. evaluate effective state/time
5. verify owning Partner credential/scope
6. verify Reservation state = HELD or PAYMENT_RETRY
7. verify Event permits continuation
8. verify no active CheckoutAttempt
9. freeze Reservation composition and terms
10. create CheckoutAttempt(ACTIVE)
11. set bounded checkout-protection deadline
12. transition Reservation -> COMMITTING
13. audit/outbox/idempotency
COMMIT
~~~

### 29.3 Event Rules

`ON_SALE`, `PAUSED`, and `SALES_CLOSED` may permit existing valid Reservations to begin checkout.

`CANCELLED` and `COMPLETED` MUST reject new protected checkout.

### 29.4 Disabled Partner

A Partner that was operationally disabled after the Reservation was created MAY continue eligible pre-existing Reservations according to graceful-disable policy, provided authentication remains valid.

---

## 31. `ReportPaymentFailure`

### 30.1 Purpose

Record a Partner's definitive statement that the active payment attempt failed.

### 30.2 Transaction

Lock Event, Reservation, and current CheckoutAttempt.

If the active attempt is valid:

- CheckoutAttempt -> `PAYMENT_FAILED`;
- if retry policy remains available:
  - Reservation -> `PAYMENT_RETRY`;
  - establish bounded retry deadline;
- otherwise:
  - release Reservation safely.

This command MUST NOT be used for an uncertain payment outcome.

---

## 32. Checkout Protection Timeout

When `COMMITTING` protection expires without accepted confirmation or safe explicit release:

- the current CheckoutAttempt becomes `UNCERTAIN`;
- Reservation becomes `RECONCILING`;
- inventory remains reserved;
- a bounded reconciliation deadline is created or applied.

This transition MAY be materialized by a worker, but effective-state guards MUST treat an overdue `COMMITTING` Reservation according to reconciliation semantics even before cleanup.

---

## 33. `ReleaseReservation`

### 32.1 Guard

Release is permitted only when:

- Reservation is not `CONFIRMED`;
- caller owns or is privileged for the Reservation;
- payment status is known safe for release.

`RECONCILING` MUST NOT be released merely to free inventory while payment outcome remains uncertain.

### 32.2 Transaction Sequence

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate
3. lock Reservation
4. evaluate effective state
5. validate safe-release authority
6. lock source Allocations
7. lock inventory in canonical order
8. Reservation -> RELEASED
9. restore every item to correct source disposition
10. update GA accounting
11. mark active CheckoutAttempt ABANDONED where applicable
12. audit/outbox/idempotency
COMMIT
~~~

### 32.3 Source-Aware Restoration

Shared source:

`RESERVED -> AVAILABLE`

Active channel Allocation source:

`RESERVED -> ALLOCATED`

Released Allocation source:

return to that Allocation's recorded release destination.

---

## 34. Reservation Expiry

Expiry uses the same source-aware transactional restoration as explicit release.

### 33.1 `HELD`

Expired `HELD`:

`HELD -> EXPIRED`

and inventory restores to source.

### 33.2 `PAYMENT_RETRY`

Expired retry:

`PAYMENT_RETRY -> EXPIRED`

and inventory restores to source.

### 33.3 `RECONCILING`

At reconciliation expiry:

- Reservation -> `EXPIRED`;
- inventory restores to source;
- later ordinary confirmation cannot reclaim it.

---

## 35. `ConfirmReservation`

### 34.1 Purpose

Create exactly one authoritative commercial Sale and corresponding ticket entitlements.

### 34.2 Transaction Sequence

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate (shared)
3. verify Event has not CANCELLED/COMPLETED in a way that forbids confirmation
4. lock Reservation
5. perform preliminary effective-state rejection if already clearly invalid
6. validate owning Partner or privileged recovery actor
7. lock source Allocations where accounting requires
8. lock all reserved units and GA pools in canonical order
9. capture authoritative `accepted_at`
10. re-evaluate Reservation deadline/state at `accepted_at`
11. verify inventory remains bound to this Reservation
12. verify Reservation is validly COMMITTING or RECONCILING
13. Reservation -> CONFIRMED
14. current CheckoutAttempt -> CONFIRMED where applicable
15. reserved units RESERVED -> SOLD
16. GA active_reserved decreases; sold_current increases
17. create exactly one Sale
18. create SaleItems from frozen ReservationItems
19. create TicketEntitlements
20. for GA quantity N, create N independently admissible TicketEntitlements
21. create one ACTIVE QRCredential per new TicketEntitlement
22. create AuditEvent(s)
23. create OutboxEvent(s)
24. persist successful idempotency result

COMMIT
~~~

### 34.3 Atomicity

No observer may see a committed state where:

- Reservation is confirmed but Sale is absent;
- Sale exists but inventory remains reserved;
- Sale exists but ticket entitlements are missing;
- ticket entitlements exist without the corresponding confirmed Sale;
- successful idempotency record exists but the Sale transaction rolled back.

### 34.4 Cancellation Race

Confirmation and `CancelEvent` are ordered by the Event lifecycle gate.

No new Sale may be created after cancellation has committed.

---

## 36. `CreateBlock`

### 35.1 Purpose

Withhold selected inventory from acquisition without granting it to a sales channel or buyer.

### 35.2 Transaction

~~~text
BEGIN

1. idempotency where externally retriable
2. lock Event lifecycle gate
3. validate admin authority
4. lock requested reserved units / GA pools
5. verify complete selection eligible for block
6. create InventoryRestriction(kind=BLOCK)
7. RESERVED? -> conflict, do not displace buyer
8. AVAILABLE -> BLOCKED
9. decrement GA available / increment blocked
10. audit/outbox
COMMIT
~~~

Bulk block is all-or-nothing by default.

---

## 37. `CreateAllocation`

### 36.1 Purpose

Withhold inventory for a defined audience/purpose.

Allocation mode SHALL be explicit:

- `CHANNEL`
- `NON_PUBLIC`

### 36.2 Transaction

Like block creation, but the resulting inventory disposition is `ALLOCATED` and the Allocation records:

- purpose;
- audience/mode;
- assigned Partner if channel-specific;
- quantity/seat membership;
- lifecycle;
- release destination semantics.

A public Partner MUST NOT acquire another Partner's channel Allocation.

---

## 38. `ReleaseBlock`

Only unconsumed blocked inventory may be released through ordinary release.

Transaction:

- lock Event;
- lock restriction;
- lock affected inventory;
- `BLOCKED -> AVAILABLE` or explicit safe destination;
- update GA accounting;
- restriction -> `RELEASED`/`CLOSED` as appropriate;
- audit/outbox.

---

## 39. `ReleaseAllocation`

Releasing an Allocation MUST NOT displace inventory already protected by Reservations.

Transaction behavior:

1. lock Event;
2. lock Allocation;
3. lock unreserved allocated inventory;
4. return immediately releasable inventory to recorded destination;
5. preserve active Reservation bindings;
6. record a release destination for inventory that later returns from those Reservations;
7. mark restriction lifecycle appropriately;
8. audit/outbox.

An Allocation may therefore be logically released while some protected child inventory remains temporarily bound to active Reservations.

---

## 40. `ReclassifyRestriction`

Block <-> Allocation reclassification is permitted only where it does not displace active buyer obligations.

Default multi-item behavior is all-or-nothing.

The destination restriction and source restriction semantics MUST be explicit.

---

## 41. `CreateNonPublicIssuance`

### 40.1 Purpose

Issue ticket entitlements from a `NON_PUBLIC` Allocation without fabricating a commercial Sale.

### 40.2 Transaction

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate
3. verify Event permits issuance
4. lock Allocation
5. lock selected inventory / GA pool
6. verify remaining allocation eligibility
7. consume allocation
8. reserved unit ALLOCATED -> ISSUED
   or GA allocated quantity -> issued_current
9. create NonPublicIssuance
10. create TicketEntitlement(s)
11. create ACTIVE QRCredential(s)
12. audit/outbox/idempotency
COMMIT
~~~

No Sale is created.

Reporting MUST preserve `ISSUED` separately from `SOLD`.

---

## 42. `PauseSales`, `ResumeSales`, `CloseSales`

Event lifecycle mutations SHALL:

- acquire Event `FOR UPDATE`;
- validate actor authorization;
- validate legal lifecycle transition;
- change Event state;
- write audit/outbox;
- commit.

They MUST NOT synchronously rewrite every Reservation or inventory row.

The Event state immediately changes future command eligibility.

---

## 43. `CancelEvent`

### 42.1 Transaction

~~~text
BEGIN

1. idempotency/privileged command identity
2. lock Event FOR UPDATE
3. validate cancellation authority and reason
4. Event -> CANCELLED
5. audit/outbox
COMMIT
~~~

### 42.2 Post-Commit Effects

Cancellation guard dominance immediately prevents:

- new holds;
- new protected checkout;
- new commercial confirmation accepted after cancellation;
- ordinary admission.

Reservation cleanup is asynchronous and state-aware:

| Reservation state | Cancellation handling |
|---|---|
| HELD | release/terminate with event-cancelled reason |
| PAYMENT_RETRY | release/terminate |
| COMMITTING | enter cancellation-aware reconciliation |
| RECONCILING | continue bounded cancellation-aware reconciliation |
| CONFIRMED | preserve Sale/Ticket history |
| RELEASED/EXPIRED | no change |

Cancellation MUST NOT be implemented as one massive transaction touching all seats and Reservations.

---

## 44. `DisablePartner`

Partner operational disable:

~~~text
BEGIN
lock Partner FOR UPDATE
Partner -> DISABLED
audit/outbox
COMMIT
~~~

New acquisition subsequently fails.

Existing Reservations remain governed by their own previously accepted rights.

---

## 45. `DisablePartnerEventAccess`

Same semantics as Partner disable but scoped to one Event access grant.

The command MUST NOT delete existing Reservations.

---

## 46. `RevokePartnerCredential`

Credential revocation:

- locks credential record;
- marks it `REVOKED`;
- records audit;
- commits.

It does not release Reservations.

A different valid credential for the same Partner or privileged recovery process MAY continue eligible pre-existing transactions.

---

## 47. `VoidTicket`

### 46.1 Transaction

~~~text
BEGIN

1. idempotency claim
2. lock Event lifecycle gate
3. lock TicketEntitlement
4. validate actor authority
5. verify Ticket = ACTIVE
6. lock active QRCredential
7. Ticket -> VOIDED
8. active credential -> REVOKED
9. audit/outbox/idempotency
COMMIT
~~~

The historical Sale or NonPublicIssuance is not altered.

Inventory is not automatically re-released.

---

## 48. `ReissueCredential`

One logical transaction:

1. lock Event/Ticket;
2. validate Ticket remains `ACTIVE`;
3. lock current active QRCredential;
4. current credential -> `SUPERSEDED`;
5. create replacement credential -> `ACTIVE`;
6. audit/outbox/idempotency;
7. commit.

At most one active credential may exist per TicketEntitlement.

---

## 49. QR Credential Representation

QR credentials SHALL use opaque, high-entropy random credential material.

Recommended pattern:

- QR contains a public TktSync validation reference or URL plus opaque token;
- database stores a secure hash/digest of the secret token where practical;
- credential lookup does not rely on customer PII;
- token does not encode mutable authority such as `admitted=false`;
- ticket/credential state is always resolved authoritatively.

Possession of decoded QR data does not authorize any mutation other than presenting the credential for validation.

---

## 50. `ReReleaseInventory`

Re-release after ticket void is a distinct privileged operation.

Required guards:

- relevant TicketEntitlement(s) are voided;
- no active entitlement currently consumes the inventory;
- no active Reservation consumes it;
- Event policy permits re-release;
- destination is explicit.

Destination may be:

- shared `AVAILABLE`;
- eligible active Allocation.

Historical Sale/Issuance remains unchanged.

---

## 51. `ValidateAndAdmit`

### 50.1 Purpose

Atomically validate a QR credential and create at most one active Admission for a single-entry ticket.

### 50.2 Transaction

~~~text
BEGIN

1. claim scan-operation idempotency
2. lock Event lifecycle gate
3. validate scanner Event authorization
4. resolve and lock TicketEntitlement
5. resolve active presented QRCredential
6. verify credential binding/state
7. evaluate Event/admission window
8. verify Ticket = ACTIVE
9. verify no ACTIVE Admission
10. create ScanAttempt(result=ADMITTED)
11. create ACTIVE Admission
12. audit/outbox/idempotency
COMMIT
~~~

### 50.3 Duplicate Race

A database-level uniqueness invariant SHALL ensure no more than one `ACTIVE` Admission per TicketEntitlement under `SINGLE_ENTRY`.

Concurrent distinct scans:

- one succeeds;
- the others return `ALREADY_ADMITTED`.

### 50.4 Same Scan Retry

The same idempotent ScanAttempt retry MUST replay the original success rather than produce a false duplicate warning.

---

## 52. Rejected Scan Attempts

Meaningful rejected scans SHOULD be persisted where required for gate operations/audit.

Examples:

- wrong Event;
- revoked credential;
- voided Ticket;
- already admitted;
- admission window closed;
- event cancelled.

A rejected ScanAttempt does not create Admission.

---

## 53. `CorrectAdmission`

A correction is privileged.

Transaction:

- lock Event;
- lock Ticket;
- lock current Admission;
- require actor authority and reason;
- `Admission ACTIVE -> REVERSED`;
- preserve original ScanAttempt;
- audit/outbox;
- commit.

A reversed Admission remains historical.

---

## 54. `ManualAdmissionOverride`

A manual override MUST:

- require Gate Supervisor or Platform Admin authority as defined by domain policy;
- require reason;
- preserve the original validation state;
- not create a second active Admission;
- atomically reverse an erroneous prior Admission if correction is part of the same workflow;
- write audit/outbox facts.

A Gate Supervisor override MUST NOT silently make wrong-event, voided, revoked, or cancelled-event credentials ordinarily valid.

---

# PART III — DATABASE ENFORCEMENT PRINCIPLES

## 55. Constraint Strategy

Application guards are mandatory but MUST be backed by database constraints where the invariant is representable relationally.

Required categories include:

- unique stable business identifiers;
- one active QR credential per TicketEntitlement;
- one active Admission per single-entry TicketEntitlement;
- one Sale per confirmed Reservation;
- non-negative GA accounting;
- valid positive GA quantities;
- unique idempotency scope;
- immutable origin relationship for TicketEntitlement;
- foreign-key ownership/scope integrity.

Exact DDL belongs to the subsequent relational schema specification.

---

## 56. Reserved Inventory Claim Enforcement

Reserved inventory current disposition and current-claim references MUST be stored such that one row cannot simultaneously point to multiple active consumers.

A state transition MUST be conditional on the row's currently locked disposition.

No application read-before-write without a lock is sufficient.

---

## 57. GA Accounting Enforcement

GA accounting MUST preserve:

~~~text
capacity =
    available
  + blocked
  + allocated_unreserved
  + active_reserved
  + sold_current
  + issued_current
~~~

The database SHOULD enforce non-negative component quantities and total-balance invariants wherever practical.

All quantity mutation occurs while the GA pool row is locked.

---

## 58. Immutable Historical Facts

The following SHALL be append-only/immutable in ordinary operation:

- Sale;
- SaleItem;
- NonPublicIssuance;
- historical AuditEvent;
- historical ScanAttempt;
- historical Admission record identity;
- outbox fact identity.

Corrections SHALL create new state/facts rather than delete history.

---

# PART IV — SECURITY ARCHITECTURE

## 59. Authentication Classes

TktSync SHALL distinguish:

1. human administrative users;
2. event staff/scanners;
3. Partner machine credentials;
4. BuyerSelectionSession capabilities.

They MUST NOT share unrestricted credentials.

---

## 60. Partner Authentication

Partner API requests SHALL use revocable machine credentials.

Credentials MUST:

- be scoped to one Partner;
- be stored securely;
- be revocable independently;
- not expose raw secrets after creation;
- be subject to PartnerEventAccess;
- support rotation.

Partner identity is not equivalent to API-key identity.

---

## 61. Human Authentication and RBAC

Human admin/scanner authentication MAY be backed by Supabase Auth or equivalent.

Authorization MUST preserve logical roles:

- `PLATFORM_ADMIN`
- `EVENT_MANAGER`
- `BOX_OFFICE`
- `GATE_SUPERVISOR`
- `SCANNER`
- `VIEWER`

Every command MUST evaluate both identity and Event scope.

---

## 62. Buyer Selection Capability

White-label buyer capability SHALL be:

- opaque or cryptographically protected;
- time bounded;
- scoped to Partner;
- scoped to Event;
- scoped to buyer/session;
- restricted to permitted Reservation operations.

It MUST NOT authorize:

- commercial confirmation;
- arbitrary Reservation access;
- Partner administration;
- event administration.

---

## 63. Data Minimization

Inventory transactions SHOULD use opaque Partner references.

TktSync MUST NOT require unrelated customer PII for a hold.

Realtime, logs, outbox payloads, QR credentials, and projections SHOULD minimize PII.

Secrets, API keys, and raw capability tokens MUST NOT be written to general application logs.

---

# PART V — FAILURE AND DEGRADATION DESIGN

## 64. Database Unavailable

If PostgreSQL authoritative state cannot be safely evaluated:

- new holds fail closed;
- confirmation does not guess;
- admission cannot claim authoritative duplicate protection;
- stale cache cannot be used to create ownership.

Temporary inability to transact is preferable to overselling.

---

## 65. Realtime Unavailable

Authoritative transactions MAY continue.

Clients may observe stale UI until refresh.

On reconnect, clients fetch current contextual availability/state.

No transaction waits for realtime delivery.

---

## 66. Worker Unavailable

Authoritative command guards continue enforcing deadlines.

Cleanup/audit projection freshness may lag.

Worker recovery resumes materialization from durable database deadlines/outbox rows.

---

## 67. Outbox Dispatcher Unavailable

Business transactions continue if PostgreSQL is healthy.

Outbox accumulates durable undispatched facts.

Dispatcher recovery retries publication.

Consumers MUST tolerate duplicates.

---

## 68. API Instance Crash During Transaction

If crash occurs before commit:

- PostgreSQL rolls back;
- operation can be retried using same idempotency key.

If crash occurs after commit but before response:

- operation is already authoritative;
- retry returns durable idempotent result.

---

## 69. Partner Timeout During Confirmation

Partner MUST retry the same confirmation idempotently or query Reservation status.

It MUST NOT create a second logical confirmation with a new order identity merely because the first response was lost.

---

## 70. Payment Outcome Unknown

Unknown payment MUST transition into or remain under reconciliation semantics.

Inventory MUST NOT be released simply to increase sellable quantity while payment status remains uncertain.

---

## 71. Scanner Connectivity Loss

MVP online scanner cannot promise authoritative duplicate prevention without database connectivity.

The UI MUST distinguish:

- authoritative rejection;
- duplicate;
- temporary authority unavailable.

Any manual admission follows privileged override policy.

---

# PART VI — OBSERVABILITY, AUDIT, AND OPERATIONS

## 72. Audit Integration

Material business mutation transactions SHALL write AuditEvent records within the same authoritative transaction where practical.

Audit MUST cover:

- lifecycle changes;
- holds/modifications;
- checkout/reconciliation transitions;
- confirmation;
- restrictions;
- non-public issuance;
- ticket void;
- QR rotation;
- admission outcomes;
- corrections/overrides;
- partner disable/revocation;
- privileged recovery;
- re-release.

Privileged actions require actor and reason.

---

## 73. Structured Application Logs

Application logs SHOULD contain:

- request correlation ID;
- operation type;
- Event ID;
- Partner ID where applicable;
- Reservation/Ticket references;
- idempotency identity hash/reference;
- transaction attempt number;
- result code;
- latency;
- lock-wait indicators where observable.

Logs MUST avoid raw secrets and unnecessary PII.

---

## 74. Metrics

The platform SHOULD expose at least:

### Transaction metrics
- hold success/conflict rate;
- hold latency;
- confirmation latency;
- release/expiry rate;
- reconciliation count/duration;
- idempotency replay/conflict rate;
- transaction retry/deadlock rate.

### Inventory metrics
- reserved-seat disposition counts;
- GA available/reserved/sold/issued counts;
- oversell invariant violations: always expected zero.

### Worker metrics
- oldest overdue Reservation not materialized;
- expiry processing lag;
- outbox backlog;
- oldest undispatched outbox age.

### Admission metrics
- admitted count;
- duplicate scan count;
- rejected scan count by reason;
- scanner authority-unavailable rate.

---

## 75. Alerts

Operational alerts SHOULD include:

- database unavailable;
- repeated transaction deadlocks above threshold;
- outbox backlog age above threshold;
- expiry/reconciliation lag above threshold;
- GA invariant failure;
- duplicate active-admission constraint failure;
- repeated failed confirmation attempts;
- abnormal Partner hold rate/hoarding pattern;
- credential abuse indicators.

An invariant violation is a high-severity incident.

---

# PART VII — PERFORMANCE AND SCALE

## 76. Performance Principle

Correctness is transactional; read scalability is projection/caching oriented.

Write paths MUST NOT weaken locks or validation merely to reduce latency.

---

## 77. Expected Contention Hotspots

Primary hotspots:

- popular reserved seats;
- high-volume GA pools;
- Event lifecycle gate during lifecycle transitions;
- admission on duplicated QR;
- large bulk allocation operations.

The architecture intentionally localizes contention to the authoritative resource being changed.

---

## 78. Event Gate Scalability

Normal transaction traffic uses a compatible shared Event lock.

Only lifecycle mutation uses exclusive locking.

This prevents every hold from serializing behind other holds while still ordering lifecycle changes.

If future scale demonstrates Event-row lock metadata as a measurable hotspot, a reviewed alternative MAY replace it, but the same ordering semantics MUST remain.

---

## 79. Large Bulk Administrative Operations

Large block/allocation changes SHOULD:

- validate requested size limits;
- lock inventory in canonical chunks only when policy permits;
- remain all-or-nothing where the requested action is defined as atomic;
- avoid holding broad locks while rendering UI or performing network calls.

No external API/network request may occur while authoritative database locks are held unless unavoidable and explicitly reviewed.

---

## 80. No Network Calls Inside Core Transactions

Authoritative PostgreSQL transactions MUST NOT wait on:

- payment providers;
- Partner APIs;
- email/SMS;
- realtime services;
- webhook receivers;
- analytics;
- external floor-plan services.

External communication occurs after commit through outbox-driven mechanisms where applicable.

This prevents long lock holding and distributed ambiguity.

---

# PART VIII — TEST AND VERIFICATION REQUIREMENTS

## 81. Transactional Test Strategy

The architecture MUST be validated by automated tests that exercise actual database concurrency, not mocks alone.

---

## 82. Reserved-Seat Contention Test

Example acceptance test:

- 100 concurrent requests attempt the same seat;
- exactly one hold succeeds;
- 99 receive inventory-unavailable/idempotent equivalent;
- exactly one active claim exists.

---

## 83. Multi-Seat Atomicity Test

Request seats A10, A11, A12 when A12 is already unavailable.

Expected:

- zero new seats held;
- no partial Reservation;
- A10/A11 remain available.

---

## 84. GA Contention Test

Pool available quantity = 10.

100 concurrent requests each request quantity 1.

Expected:

- exactly 10 successful held units;
- 90 failures;
- available never negative;
- accounting equation holds.

---

## 85. Mixed Inventory Atomicity Test

Request:

- reserved A12;
- GA quantity 3.

If either component cannot be acquired:

- neither component is consumed.

---

## 86. Hold vs Block Race Test

Simultaneous hold and block for same available seat.

Expected:

- exactly one succeeds;
- final state corresponds to winning committed transition;
- no admin override silently steals a successful hold.

---

## 87. Confirmation vs Expiry Boundary Test

Cases MUST include:

- confirmation accepted just before deadline;
- confirmation waits on lock until after deadline before guard evaluation;
- reconciliation expiry vs late confirmation;
- duplicate confirmation retry.

The accepted-before-deadline semantics MUST be verified.

---

## 88. Confirmation vs Cancellation Test

Two orderings:

1. confirmation commits first;
2. cancellation commits first.

Expected outcomes MUST match Section 14 and logical-domain cancellation rules.

---

## 89. Allocation Restoration Test

Channel-allocated seat held then released:

- returns to active Allocation, not shared availability.

Allocation released while seat remains held:

- buyer right remains;
- later Reservation release goes to recorded Allocation release destination.

---

## 90. Idempotency Tests

Every mutating API MUST test:

- same key + same request;
- same key + different request;
- concurrent duplicate first request;
- response lost after commit;
- transaction rollback before idempotency completion.

---

## 91. Scanner Concurrency Test

100 distinct simultaneous scans of one valid single-entry QR.

Expected:

- exactly one active Admission;
- one `ADMITTED`;
- all others `ALREADY_ADMITTED`.

---

## 92. Scanner Retry Test

Retry same ScanAttempt idempotency identity after original success.

Expected:

- replay original `ADMITTED`;
- do not report duplicate fraud.

---

## 93. Worker Delay Test

Disable expiry worker beyond multiple hold deadlines.

Expected:

- expired hold cannot begin checkout;
- no command treats worker delay as extended entitlement;
- worker later materializes correct state.

---

## 94. Realtime Failure Test

Disable realtime transport.

Expected:

- holds/confirmation remain correct;
- UI may become stale;
- authoritative refresh restores correct state.

---

## 95. Database Constraint Tests

Tests MUST prove database constraints reject impossible states even if application validation is bypassed in test fixtures.

---

# PART IX — IMPLEMENTATION BOUNDARIES AND HANDOFF

## 96. Module Boundaries

The backend SHALL preserve modules equivalent to:

- Venue & Layout;
- Event & Inventory;
- Reservation & Checkout;
- Restrictions & Allocations;
- Ticketing & Entitlements;
- Admission;
- Partner & Authorization;
- Audit & Outbox;
- Reporting & Projections.

Modules MAY call each other in-process through application/domain interfaces.

Modules MUST NOT bypass another module's invariants through uncontrolled direct mutation.

---

## 97. Repository Shape

A monorepo is recommended.

Representative structure:

~~~text
apps/
  api/
  worker/
  admin-web/
  selector-web/
  scanner-web/

packages/
  domain/
  contracts/
  database/
  auth/
  observability/
  ui/
~~~

Exact folder names are implementation details; module ownership and boundaries are not.

---

## 98. Database Migration Ownership

All schema changes SHALL be migration-controlled and reviewed.

No production schema mutation may occur through ad hoc runtime table creation.

The subsequent Relational Data Model Specification SHALL define tables, keys, indexes, constraints, and migration order.

---

## 99. API Contract Handoff

The subsequent API & Partner Integration Contract MUST derive endpoints from the command semantics in this document.

The API specification MUST NOT redefine transaction behavior.

In particular:

- availability remains non-authoritative;
- hold remains all-or-nothing;
- checkout protection precedes chargeable payment;
- confirmation is idempotent;
- release is source-aware;
- business errors remain machine-readable.

---

# PART X — CONFIGURATION, NOT ASSUMPTIONS

## 100. Configurable Values

The following are intentionally configuration values and MUST NOT be silently hard-coded as domain semantics:

- ordinary hold duration;
- checkout-protection duration;
- payment-retry duration;
- reconciliation duration;
- maximum total Reservation lifetime;
- maximum quantity per hold;
- maximum active Reservations per Partner/buyer;
- rate limits;
- Event sale window;
- admission window;
- allocation categories;
- whether voided inventory may be re-released;
- re-entry policy.

Each deployment MUST define these values explicitly through configuration with safe defaults.

No configured value may defeat bounded-lifetime or safety invariants.

---

## 101. Architecture Decisions Frozen by This Specification

The following are not left open to lower-level implementation:

1. Modular monolith for authoritative MVP business logic.
2. One authoritative PostgreSQL transactional database.
3. PostgreSQL row locks and constraints, not distributed locks, govern inventory ownership.
4. `READ COMMITTED` plus explicit locking is the default isolation model.
5. Canonical lock ordering is mandatory.
6. Event lifecycle transitions are ordered against in-flight commands through an Event lifecycle gate.
7. Server/database time is authoritative.
8. Guard time is evaluated after relevant lock acquisition.
9. Background workers materialize expiry but do not define expiry.
10. Idempotency is durable and transactional.
11. Transactional outbox is the durable source for post-commit publication.
12. Realtime is non-authoritative and caller-contextual availability is re-fetched.
13. No external cache is required for correctness.
14. No external network call is made inside core authoritative transactions.
15. Confirmation is one PostgreSQL business transaction.
16. Admission is one PostgreSQL business transaction.
17. Non-public issuance is distinct from Sale.
18. QR credential identity is distinct from TicketEntitlement identity.
19. Partner operational disable is distinct from credential revocation.
20. Event cancellation uses guard dominance and state-aware asynchronous cleanup rather than mass synchronous mutation.

Any change to these decisions requires an explicit architecture revision and policy/domain compatibility review.

---

# PART XI — POLICY AND DOMAIN TRACEABILITY

## 102. Traceability Matrix

| Governing requirement | Architecture enforcement |
|---|---|
| TktSync is sole inventory authority | PostgreSQL is sole transactional source of truth |
| Availability does not reserve | availability is derived read; hold revalidates under locks |
| Multi-item all-or-nothing | one hold transaction locks and validates complete selection |
| No silent substitution | transaction uses explicit requested selection; conflict aborts |
| Reserved unit one current claim | row disposition/current claim + lock + constraints |
| GA never negative | pool row lock + accounting constraints |
| Shared Partner neutrality | no hidden priority; atomic acquisition determines winner |
| Channel allocation isolation | Allocation eligibility checked/locked before inventory claim |
| Source-aware release | ReservationItem persists acquisition source/release destination |
| Protected checkout before payment | `BeginCheckout` transaction precedes Partner payment |
| Original hold expiry does not defeat committing buyer | Reservation state/time guards |
| Retry/reconciliation bounded | persisted deadlines + max lifetime |
| Reconciliation inventory unavailable | remains `RESERVED`/active_reserved |
| Late confirm cannot reclaim expired inventory | Reservation terminal guard + inventory binding check |
| Exactly one Sale | unique Sale-per-Reservation + confirmation transaction |
| `ISSUED` distinct from `SOLD` | NonPublicIssuance transaction and separate disposition |
| Price snapshot preserved | ReservationItem snapshot written during hold |
| Admin cannot steal hold | inventory locks/state guard reject block on reserved inventory |
| Event lifecycle separate | Event row gate + independent inventory state |
| Cancellation ordering | shared/exclusive Event lifecycle locking |
| Partner disable graceful | Partner/access gate only blocks expansion; Reservation rights retained |
| Credential revocation separate | independent PartnerCredential lifecycle |
| Idempotent mutations | durable transactional idempotency table |
| Network timeout not failure proof | stable replay/queryable result |
| Server time authoritative | PostgreSQL `clock_timestamp()`-class guard time |
| Worker delay cannot extend rights | command-level effective-state evaluation |
| Ticket and QR separate | independent TicketEntitlement/QRCredential persistence |
| One active QR | database uniqueness + rotation transaction |
| One active admission | database uniqueness + admission transaction |
| Same scan retry same result | scan idempotency committed with Admission |
| Audit append-only | AuditEvent inserted with business transaction |
| Realtime freshness only | transactional outbox + refresh-on-reconnect |
| Reports/exports not authority | read-only projection/report paths |
| Fail closed when authority unavailable | database-dependent mutations reject rather than infer stale state |
| Customer PII minimized | opaque refs, scoped capability, minimal event payloads |
| Buyer client lacks Partner authority | separate BuyerSelectionSession capability |
| No hidden cross-Partner transaction disclosure | caller-contextual availability projection |

---

## 103. Logical Domain Handoff Coverage

This architecture explicitly implements all implementation handoff requirements from the Logical Domain Specification:

1. reservation multi-item atomicity;
2. GA non-negative capacity;
3. source-aware Reservation release;
4. state/time guards independent of cleanup-worker timing;
5. confirmation idempotency and exactly-one Sale creation;
6. Ticket/QRCredential separation;
7. scan concurrency and idempotency;
8. append-only auditability;
9. caller-contextual availability without duplicated truth;
10. Event cancellation behavior;
11. Partner isolation and credential revocation;
12. realtime projection consistency;
13. fail-closed behavior when authoritative inventory is unavailable.

No known handoff requirement remains architecturally unassigned.

---

# PART XII — REVIEW FINDINGS AND DRIFT RESOLUTION

## 104. Review Against Platform Process & Policy Standard

The architecture was reviewed against the approved platform policy.

The following potentially dangerous implementation drifts are explicitly prevented:

- using availability cache as ownership;
- allowing hold expiry worker timing to determine rights;
- charging before checkout protection;
- immediate resale after uncertain payment;
- partial mixed-inventory holds;
- returning channel allocation inventory to shared pool on hold expiry;
- admin block overriding an active buyer hold;
- treating comp/VIP issuance as a commercial Sale;
- using ticket void as inventory release;
- using QR identity as Ticket identity;
- making ScanAttempt equivalent to Admission;
- treating Partner disable as credential revocation;
- treating Event cancellation as a mass state rewrite;
- allowing post-cancellation new Sale confirmation;
- publishing realtime state before authoritative commit.

---

## 105. Review Against Logical Domain Specification

The architecture was reviewed against the Logical Domain Specification and preserves its independent state dimensions.

No architecture component introduces a single overloaded status combining:

- Event lifecycle;
- Reservation lifecycle;
- inventory disposition;
- restriction state;
- Ticket state;
- credential state;
- Admission state.

Cross-domain transitions are coordinated transactionally without collapsing identities.

---

## 106. Technical Brief Alignment

The architecture remains aligned with the original product brief:

- one central inventory truth;
- multiple Partner sales channels;
- reserved and GA inventory;
- atomic holds;
- hold expiry;
- confirmation/release;
- white-label seat selection;
- blocks/allocations;
- QR generation;
- duplicate-scan prevention;
- audit;
- Partner reporting;
- mobile-web scanner;
- Supabase/PostgreSQL and realtime;
- web hosting on Vercel/Render-class infrastructure;
- third-party floor-plan engine.

The architecture extends the brief's simple Hold -> Confirm/Release model only where the approved policy already requires protected checkout, payment retry, and reconciliation to avoid customer harm.

---

## 107. Explicitly Deferred Details

The following belong to later specifications and are not silently assumed here:

### Relational Data Model Specification
- exact table names;
- column types;
- indexes;
- partial unique indexes;
- foreign keys;
- check constraints;
- migration SQL.

### API & Partner Integration Contract
- HTTP routes;
- request/response JSON;
- HTTP status mappings;
- API versioning;
- webhook contracts where required.

### Realtime/Event Contract
- exact fact schemas;
- channel naming;
- sequence/revision format;
- subscriber authorization.

### Security Implementation Specification
- exact key formats;
- token cryptography;
- credential rotation schedules;
- rate-limit values;
- session lifetimes.

These later documents MUST implement this specification; they may not alter it.

---

## 108. Final Architecture Summary

The authoritative architecture is:

~~~text
                         Partner / Admin / Buyer / Scanner
                                      │
                                      ▼
                          ┌─────────────────────────┐
                          │    TktSync Core API     │
                          │    Modular Monolith     │
                          └────────────┬────────────┘
                                       │
                           authoritative transaction
                                       │
                                       ▼
                          ┌─────────────────────────┐
                          │ PostgreSQL / Supabase   │
                          │                         │
                          │ Row locks               │
                          │ Constraints             │
                          │ Idempotency             │
                          │ Audit                   │
                          │ Transactional Outbox    │
                          └────────────┬────────────┘
                                       │
                         committed facts / deadlines
                                       │
                    ┌──────────────────┴──────────────────┐
                    ▼                                     ▼
             Background Worker                    Outbox Dispatcher
          expiry / reconciliation                realtime / projections
                    │                                     │
                    └───────────────┬─────────────────────┘
                                    ▼
                             Derived Read Surfaces
~~~

The governing write principle is:

> **Every irreversible inventory, sale, entitlement, or admission decision is made against PostgreSQL authoritative state inside an explicit transaction whose locks, guards, and constraints correspond to the logical domain invariants.**

The governing failure principle is:

> **When authoritative state cannot be established safely, TktSync fails closed rather than infer ownership from cache, realtime, client state, or worker timing.**

The governing distribution principle is:

> **Domain events are post-commit facts. Realtime, reports, caches, and projections may become stale; authoritative ownership may not.**

The governing evolution principle is:

> **Future scaling may change deployment topology, but it must not change transactional semantics without an explicit architecture and policy review.**

---

**End of Document**

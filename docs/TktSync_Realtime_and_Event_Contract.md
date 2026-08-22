# TktSync Realtime & Event Contract

**Document status:** Governing Realtime & Asynchronous Event Contract  
**Applies to:** TktSync MVP browser surfaces and compatible Partner integrations  
**Version:** 1.0  
**Date:** 20 August 2026  
**Classification:** Confidential

**Normative parents, in precedence order:**
1. TktSync Platform Process & Policy Standard v1.0
2. TktSync Logical Domain Specification v1.0
3. TktSync System Architecture & Transactional Design Specification v1.0
4. TktSync Relational Data Model & Schema Specification v1.0
5. TktSync API & Partner Integration Contract v1.0
6. TktSync Security & Authentication Specification v1.0

**Product basis:** TktSync Technical Brief (2026)

---

## 1. Purpose

This document defines the asynchronous event and realtime behavior of TktSync.

It governs:

- the relationship between authoritative transactions and publishable events;
- event naming and versioning;
- browser realtime delivery;
- Partner webhook delivery;
- event audience and privacy boundaries;
- deduplication, retry, ordering, and acknowledgement semantics;
- reconnect and resynchronization behavior;
- failure/degradation behavior;
- delivery observability and auditability;
- the boundary between asynchronous notification and authoritative API state.

This contract does not redefine inventory ownership, Reservation state transitions, Sale confirmation, Ticket validity, or Admission truth. Those remain governed by the upstream policy, domain, architecture, schema, and synchronous API contracts.

---

## 2. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

---

## 3. Governing Principle

> **Realtime and asynchronous events communicate committed facts and freshness signals. They never create, transfer, confirm, release, or invalidate authoritative business ownership by themselves.**

A consumer MUST NOT infer irreversible TktSync state solely from receipt, non-receipt, ordering, or timing of an asynchronous message.

Authoritative recovery always occurs through the synchronous TktSync API and PostgreSQL-backed state.

---

# PART I — EVENT SOURCE AND PIPELINE

## 4. Single Event Source

All externally publishable realtime/webhook events SHALL originate from committed `outbox_events` facts created in the same PostgreSQL transaction as the authoritative business mutation.

The flow is:

~~~text
Authoritative PostgreSQL transaction
            |
            | writes business state + AuditEvent + OutboxEvent
            v
          COMMIT
            |
            v
      Outbox Dispatcher
        /           \
       /             \
Browser realtime   Partner webhook fan-out
~~~

No browser or Partner event may be published from an uncommitted domain mutation.

No publication failure may roll back an already committed business transaction.

---

## 5. Internal Domain Fact vs External Event

An internal domain fact and an externally delivered event are not necessarily one-to-one.

The dispatcher MAY:

- sanitize internal data;
- omit private fields;
- coalesce high-frequency invalidations;
- map one internal fact to different audience-specific messages;
- suppress a fact for an audience that has no legitimate need to know it.

The dispatcher MUST NOT:

- invent a business state that did not commit;
- transform `COMMITTING` into `CONFIRMED`;
- expose another Partner's private transaction data;
- expose raw secrets or credential material;
- treat a notification as authority.

---

## 6. Canonical External Event Envelope

Every externally delivered event SHALL use the same logical envelope.

~~~json
{
  "id": "ev_...",
  "type": "reservation.expired",
  "schema_version": 1,
  "occurred_at": "2026-08-20T21:15:32.421Z",
  "event_id": "evt_...",
  "data": {}
}
~~~

### 6.1 `id`

- globally unique publishable fact identity;
- stable across retries/delivery attempts;
- used by consumers for deduplication;
- MUST NOT change when the same event is redelivered.

The preferred source is the immutable outbox `fact_id` represented through the public ID layer.

### 6.2 `type`

Stable semantic event name.

### 6.3 `schema_version`

Integer version of the payload schema for that event type.

### 6.4 `occurred_at`

Server-authoritative time associated with the committed business fact.

### 6.5 `event_id`

The TktSync Event to which the fact belongs, where applicable.

### 6.6 `data`

Audience-sanitized event-specific payload.

---

## 7. No Public Ordering Cursor

The database outbox insertion sequence is an internal dispatch aid and MUST NOT be exposed as a gap-free public event cursor.

Reason:

- concurrent transactions can allocate database sequence values before commit;
- transaction commit order can differ from sequence allocation order;
- exposing that sequence as a replay cursor could cause a consumer to skip a later-visible lower-sequence fact.

Therefore:

- event `id` is the deduplication identity;
- `occurred_at` is descriptive time, not a strict total-order guarantee;
- browser clients resynchronize from authoritative API state after interruption;
- Partner webhooks do not promise strict cross-event ordering;
- no consumer may use a numeric outbox sequence to reconstruct ownership.

If a future durable public replay feed is added, it MUST introduce an independently reviewed cursor model that cannot skip concurrently committed events.

---

# PART II — EVENT CATALOG

## 8. Canonical Domain-to-External Event Types

The following external event types are approved.

### 8.1 Event lifecycle

- `event.opened_for_sale`
- `event.paused`
- `event.resumed`
- `event.sales_closed`
- `event.cancelled`
- `event.completed`

### 8.2 Reservation lifecycle

- `reservation.held`
- `reservation.modified`
- `reservation.checkout_started`
- `reservation.payment_retry_opened`
- `reservation.reconciling`
- `reservation.confirmed`
- `reservation.released`
- `reservation.expired`

### 8.3 Inventory restrictions

- `inventory.blocked`
- `inventory.block_released`
- `inventory.allocated`
- `inventory.allocation_released`
- `inventory.non_public_issued`

### 8.4 Ticketing

- `ticket.issued`
- `ticket.voided`
- `credential.issued`
- `credential.superseded`
- `credential.revoked`

### 8.5 Admission

- `admission.granted`
- `admission.rejected`
- `admission.corrected`
- `admission.manual_override_granted`

Not every audience receives every canonical event.

---

## 9. Browser Invalidation Events

For high-frequency browser freshness, TktSync MAY deliver reduced invalidation events rather than complete domain facts.

Approved browser-oriented event types include:

- `inventory.changed`;
- `event.lifecycle_changed`;
- `reservation.changed`;
- `ticket.changed`;
- `admission.changed`.

These are projection notifications.

For example:

~~~json
{
  "id": "ev_...",
  "type": "inventory.changed",
  "schema_version": 1,
  "occurred_at": "2026-08-20T21:15:32.421Z",
  "event_id": "evt_...",
  "data": {
    "scope": "event"
  }
}
~~~

The client then fetches current caller-contextual availability.

The message MUST NOT claim that a named seat or quantity remains acquirable unless the payload is specifically authorized and the contract explicitly defines that value as advisory.

---

## 10. Event Payload Minimization

Event payloads SHOULD contain only the information needed to identify what changed and what authoritative resource should be refreshed.

They MUST NOT contain:

- Partner API keys;
- BuyerSelectionSession capabilities;
- Reservation continuation tokens;
- raw QR payloads;
- webhook signing secrets;
- payment card or processor secrets;
- unrelated customer PII;
- another Partner's private correlation identifiers.

---

# PART III — BROWSER REALTIME

## 11. Browser Realtime Transport

The MVP browser realtime contract SHALL be transport-independent at the semantic layer.

The approved initial transport is a **Go-managed server-to-client stream** fed by committed outbox dispatch.

A Server-Sent Events compatible stream over authenticated `fetch()` is the preferred MVP implementation because browser use cases are predominantly server-to-client invalidations.

Representative infrastructure endpoint:

~~~text
GET /api/v1/realtime/stream
~~~

This endpoint is governed by this document and the Security & Authentication Specification rather than by Partner business-command semantics.

Supabase Realtime MAY replace or supplement the physical browser transport later, provided:

- private authorization is preserved;
- only sanitized outbox-derived events are published;
- browser clients never receive direct authoritative-table write access;
- reconnect/resync semantics remain unchanged.

---

## 12. Internal Browser Fan-Out

The durable source is the outbox.

The physical fan-out from the dispatcher to active Go API instances MAY use an ephemeral mechanism such as PostgreSQL `LISTEN/NOTIFY` or an equivalent managed realtime transport.

This internal fan-out is allowed to be non-durable because:

- the outbox remains durable;
- browser realtime is explicitly non-authoritative;
- browser clients resynchronize on reconnect;
- loss of a transient notification affects freshness only.

An ephemeral fan-out mechanism MUST NOT become a source of business truth.

---

## 13. Browser Audience Classes

The stream authorizes one of the following principal classes:

1. Admin/Event Staff;
2. White-label BuyerSelectionSession;
3. Scanner.

Each stream is scoped to the authenticated principal's current authority.

### 13.1 Admin/Event Staff

May receive Event-scoped operational invalidations appropriate to role.

Examples:

- inventory changed;
- Event lifecycle changed;
- Reservation changed;
- Ticket changed;
- Admission changed.

Detailed private payloads remain role-limited.

### 13.2 White-label BuyerSelectionSession

May receive only:

- coarse Event availability invalidation;
- Event lifecycle state relevant to purchase eligibility;
- `reservation.changed` for its own Reservation/session context.

It MUST NOT receive:

- other buyers' Reservation identities;
- Partner administration events;
- other Partner Allocation details;
- Ticket/Sale information outside its own flow.

### 13.3 Scanner

Scanner realtime SHOULD remain narrow.

It MAY receive:

- Event cancellation;
- admission-window/lifecycle invalidation;
- operator-visible gate configuration invalidation.

A scanner MUST NOT treat a stream message as an admission decision. Every actual scan still calls the authoritative Admission API.

---

## 14. Browser Subscription Topics

Logical topics MAY include:

~~~text
admin:event:{event_id}
selection:{selection_session_id}
scanner:event:{event_id}
event:{event_id}:availability
~~~

Topic names are routing metadata, not secrets.

Possession or knowledge of a topic name does not authorize subscription.

The server MUST resolve the authenticated principal and verify scope before delivering events.

---

## 15. Browser Delivery Semantics

Browser realtime is:

- best-effort;
- post-commit;
- duplicate-tolerant;
- not gap-free;
- not a total-order event log;
- not a replay mechanism.

A browser MAY receive the same event `id` more than once.

Clients SHOULD retain a small in-memory deduplication set for recently observed event IDs.

---

## 16. Reconnect and Resynchronization

On any of the following:

- stream disconnect;
- laptop sleep/resume;
- browser network transition;
- authentication refresh;
- suspected message gap;
- server reconnect;

client behavior SHALL be:

~~~text
Reconnect authentication
        |
        v
Re-fetch authoritative/contextual API state
        |
        v
Re-establish realtime stream
~~~

The client MUST NOT attempt to infer all missed state transitions from event delivery history.

The white-label selector SHOULD also perform a periodic authoritative availability resynchronization while connected. The interval is a frontend/runtime configuration value; it exists to bound UI staleness if an ephemeral realtime notification is lost and does not create ownership.

---

## 17. Backpressure

Realtime stream delivery MUST protect API/runtime health.

If a browser client cannot consume events fast enough:

- low-value invalidations MAY be coalesced;
- the connection MAY be terminated;
- the client then resynchronizes through the API.

TktSync MUST NOT delay authoritative database transactions to wait for a slow realtime consumer.

---

# PART IV — PARTNER WEBHOOKS

## 18. Partner Webhook Purpose

Partner webhooks provide durable asynchronous notification to Partner backend systems.

They complement the synchronous Partner API.

They do not replace:

- Reservation status queries;
- availability queries;
- idempotent command retry;
- authoritative Ticket/credential queries.

---

## 19. Webhook Configuration

Self-serve Partner onboarding is outside the MVP.

Webhook endpoint configuration MAY therefore be managed by a Platform Admin during Partner integration.

Each endpoint SHALL define:

- owning Partner;
- HTTPS destination URL;
- active/disabled state;
- subscribed event types;
- one current signing secret;
- optional previous signing secret during bounded rotation overlap.

Multiple endpoints per Partner MAY be supported.

---

## 20. Partner Webhook Audience

A Partner may receive only facts it is authorized to know.

### 20.1 Reservation events

Delivered only to the Partner that owns the Reservation.

### 20.2 Sale events

Delivered only to the Partner whose confirmed Reservation produced the Sale.

### 20.3 Ticket/Credential events

Delivered to the commercial origin Partner for Partner-owned commercial Tickets, subject to the API permission model.

Raw QR payloads MUST NOT be included.

### 20.4 Event lifecycle events

May be delivered to Partners with applicable Event access or historical transaction relevance, as defined by the Event/Partner relationship.

### 20.5 Admission events

MAY be offered as an explicit opt-in event family for the Partner's own commercial Tickets.

Admission webhook payloads MUST remain PII-minimized.

### 20.6 Inventory changes

Per-seat/per-hold inventory webhooks are not required for the MVP and SHOULD NOT be enabled by default because they can produce extreme event volume and leak transaction activity.

Partners obtain current inventory through the authoritative availability API.

A future coarse `inventory.changed` Partner webhook MAY be introduced as an invalidation signal without changing ownership semantics.

---

## 21. Webhook Event Example

~~~json
{
  "id": "ev_01J...",
  "type": "reservation.expired",
  "schema_version": 1,
  "occurred_at": "2026-08-20T21:15:32.421Z",
  "event_id": "evt_01J...",
  "data": {
    "reservation_id": "res_01J...",
    "status": "EXPIRED",
    "terminal_reason": "HOLD_EXPIRED"
  }
}
~~~

Event payloads are intentionally smaller than the complete API resource.

Partners SHOULD fetch the authoritative resource when they need current complete state.

---

## 22. Delivery Guarantee

Webhook delivery is **at least once**.

TktSync does not promise exactly-once HTTP delivery.

Partners MUST deduplicate by event `id`.

The same `id` may be delivered multiple times due to retry or dispatcher recovery.

---

## 23. Ordering

TktSync does not guarantee strict ordering between distinct webhook events.

Examples of legitimate reordering include:

- an earlier delivery fails and is retried after a later delivery succeeds;
- different endpoints have different retry timing;
- different worker processes handle delivery independently.

Consumers MUST use authoritative resource state when event order affects behavior.

No Partner should implement logic equivalent to:

> "I saw `reservation.reconciling`, therefore I may ignore a later API response saying the Reservation is confirmed."

The API always wins.

---

## 24. Webhook Request Headers

Each delivery SHALL include headers equivalent to:

~~~http
Content-Type: application/json
User-Agent: TktSync-Webhooks/1
TktSync-Event-Id: ev_...
TktSync-Delivery-Id: del_...
TktSync-Signature: t=1787261043,v1=<signature>[,v1=<rotation-overlap-signature>]
~~~

`TktSync-Event-Id` remains stable for the logical event.

`TktSync-Delivery-Id` identifies the Partner endpoint/event delivery record and remains stable across retry attempts for that endpoint.

The signature timestamp is generated per HTTP attempt.

---

## 25. Webhook Signature

Webhook signing SHALL use HMAC-SHA-256 over the exact raw request body.

Canonical signed input:

~~~text
<unix_timestamp> + "." + <raw_request_body_bytes>
~~~

Verification rules are defined fully in the Security & Authentication Specification.

---

## 26. Webhook Acknowledgement

Any HTTP `2xx` response acknowledges the delivery.

The response body is ignored for business semantics.

The following are not acknowledgements:

- network timeout;
- TLS failure;
- DNS failure;
- HTTP `3xx`;
- HTTP `4xx`;
- HTTP `5xx`.

Redirects MUST NOT be automatically followed by the webhook sender.

---

## 27. Retry Policy

Failed webhook deliveries SHALL be retried using bounded exponential backoff with jitter.

The exact values are deployment configuration and MUST be documented in the Partner developer surface.

Configuration SHALL include:

- request timeout;
- maximum attempts;
- retry schedule/backoff bounds;
- total delivery retry window;
- endpoint failure-disable threshold;
- delivery record retention.

These values are configuration parameters, not business-state semantics.

A retry schedule MUST be bounded and MUST NOT block or delay authoritative business transactions.

---

## 28. Dead-Letter Behavior

When the configured retry policy is exhausted:

- delivery becomes `DEAD_LETTER` or equivalent;
- the committed business fact remains authoritative;
- the endpoint MAY be operationally disabled after configured repeated failure criteria;
- the failure becomes observable to Platform Admin operations;
- manual replay MAY be supported and MUST be auditable.

Manual webhook replay re-delivers the same immutable event `id`; it does not regenerate the business transaction.

---

## 29. Partner Recovery

A missing webhook is not proof that an operation did not occur.

Partners recover by:

- retrying an idempotent command using the same idempotency key; or
- querying the authoritative Reservation/Sale/Ticket/Event state.

The MVP does not expose the internal outbox sequence as a public replay cursor.

---

# PART V — WEBHOOK NETWORK SAFETY

## 30. HTTPS Requirement

Production webhook destinations MUST use HTTPS.

TLS certificate validation MUST remain enabled.

Development/staging exceptions, if any, are environment-specific and MUST NOT weaken production configuration.

---

## 31. SSRF Protection

Partner-controlled webhook URLs create a server-side request forgery risk.

The webhook sender MUST protect against this risk.

Production destination validation SHALL reject or prevent connection to:

- loopback addresses;
- link-local addresses;
- private RFC1918 networks;
- cloud metadata endpoints;
- multicast/reserved ranges;
- unsupported URL schemes;
- credentials embedded in the URL.

DNS resolution MUST be validated at connection time sufficiently to prevent DNS rebinding from bypassing destination policy.

IP literals SHOULD be rejected in production unless explicitly approved through a privileged integration process.

Redirect following is disabled.

---

## 32. Webhook Response Handling

The webhook sender SHOULD cap response body read size and MUST NOT log unrestricted Partner response content.

Operational logs MAY record:

- status code;
- latency;
- error class;
- bounded sanitized response excerpt where necessary.

Secrets or customer data MUST NOT be copied into general logs.

---

# PART VI — EVENT VERSIONING

## 33. Event Type Stability

Once a public event type is published, its semantic meaning MUST remain stable within the major contract version.

Breaking semantic change requires:

- new event type; or
- new `schema_version` with documented migration behavior.

---

## 34. Additive Changes

The following are normally backward-compatible:

- adding optional fields;
- adding new event types;
- adding new webhook subscription options.

Consumers MUST ignore unknown optional fields.

A Partner endpoint receives only event types to which it is explicitly subscribed unless it opts into a future wildcard mode.

---

## 35. Removing Fields

A required field MUST NOT be removed or change type within the same schema version.

---

# PART VII — FAILURE AND DEGRADATION

## 36. Realtime Transport Unavailable

Authoritative TktSync transactions continue when PostgreSQL is healthy.

Browser UI may become stale until refresh.

Clients re-fetch current state after reconnect.

---

## 37. Outbox Dispatcher Unavailable

Committed outbox facts remain durable.

Business transactions continue.

When the dispatcher returns:

- pending webhook delivery records are created/retried;
- realtime freshness resumes.

No business state is reconstructed from delivery state.

---

## 38. Webhook Endpoint Unavailable

Only that endpoint's notification freshness is affected.

No inventory, Reservation, Sale, Ticket, or Admission state is rolled back.

---

## 39. Duplicate Dispatcher Work

If multiple workers observe the same outbox fact:

- fan-out record creation MUST be idempotent;
- Partner delivery uniqueness MUST prevent duplicate logical delivery records for the same endpoint/event pair;
- HTTP retries may still produce duplicate physical deliveries under at-least-once semantics.

---

# PART VIII — RELATIONAL REQUIREMENTS

## 40. Required Schema Support

The following relational support is required by this contract:

- immutable `outbox_events` fact identity/payload;
- `partner_webhook_endpoints`;
- Partner webhook event subscriptions;
- versioned encrypted webhook signing secrets;
- durable per-endpoint webhook delivery records;
- optional append-only webhook delivery-attempt records.

The Relational Data Model & Schema Specification SHALL contain these structures.

---

## 41. Outbox Dispatch State Semantics

The outbox row's delivery metadata describes whether the asynchronous dispatcher has processed/fanned out the fact.

It MUST NOT mean:

> "every Partner and browser has received this event."

Per-Partner webhook delivery status lives in webhook delivery records.

Browser receipt is intentionally not persisted as authoritative state.

The internal outbox insertion sequence MUST NOT be advertised as a public gap-free replay cursor.

---

# PART IX — OBSERVABILITY

## 42. Realtime Metrics

The platform SHOULD measure:

- active browser streams;
- realtime publish rate;
- dropped/coalesced browser invalidations;
- stream reconnect rate;
- outbox processing lag.

---

## 43. Webhook Metrics

The platform SHOULD measure:

- pending deliveries;
- successful delivery rate;
- retry count;
- oldest pending delivery age;
- endpoint timeout rate;
- endpoint failure rate;
- dead-letter count;
- manual replay count.

---

## 44. Alerts

Operational alerts SHOULD cover:

- growing outbox backlog;
- repeated dispatcher failure;
- webhook dead-letter accumulation;
- repeated endpoint SSRF/network-policy rejection;
- unusual signature/configuration errors;
- persistent browser realtime outage where expected.

---

# PART X — TEST REQUIREMENTS

## 45. Post-Commit Publication Test

No external event may be observed for a business transaction that rolls back.

A committed transaction with an outbox fact MUST remain discoverable to the dispatcher after process restart.

---

## 46. Browser Disconnect Test

Disconnect browser realtime during multiple inventory changes.

On reconnect:

- client refreshes authoritative state;
- current inventory is correct even if intermediate notifications were missed.

---

## 47. Duplicate Event Test

Deliver the same browser/webhook event twice.

Consumer behavior must remain safe through event-ID deduplication or authoritative re-fetch.

---

## 48. Webhook Retry Test

Simulate:

- timeout;
- `500`;
- dispatcher crash after sending but before recording acknowledgement.

Expected:

- logical event remains one fact;
- HTTP delivery may repeat;
- Partner deduplication by event ID remains valid;
- business state remains unchanged.

---

## 49. Cross-Partner Privacy Test

Create two Partners with different Reservations/Allocations.

Verify:

- Partner A never receives Partner B Reservation identifiers or correlation values;
- browser selection session A receives no selection-session event for B;
- event availability invalidation does not expose private acquisition source.

---

## 50. SSRF Test

Webhook URL validation MUST reject representative targets including:

- `http://127.0.0.1/...`;
- private network IPs;
- link-local/cloud metadata addresses;
- URL credentials;
- redirect chains toward forbidden destinations.

---

## 51. Signature Retry Test

The same event redelivered later:

- keeps the same Event ID and Delivery ID;
- uses a fresh signature timestamp/signature for the new HTTP attempt;
- verifies successfully under the active/overlap signing secret policy.

---

# PART XI — CROSS-DOCUMENT TRACEABILITY

## 52. Technical Brief Alignment

This contract preserves the Technical Brief requirements that:

- inventory updates take effect across multiple Partner channels in real time;
- Supabase/PostgreSQL remains the central infrastructure baseline;
- Partner integration stays lightweight;
- TktSync remains one inventory/validation authority;
- real-time behavior does not move payment/customer ownership into TktSync.

"Live updates" is implemented as committed-state freshness, not as a second ownership mechanism.

---

## 53. Platform Policy Alignment

- realtime never determines ownership;
- availability remains informational;
- caller privacy is preserved;
- customer PII is minimized;
- ticket/credential distinction is preserved;
- Event cancellation and Reservation state remain authoritative API/database facts;
- ambiguous delivery failure never causes an irreversible business guess.

---

## 54. Logical Domain Alignment

Every publishable event originates from a post-commit domain fact.

Canonical event names preserve separate Event, Reservation, restriction, Ticket/Credential, and Admission dimensions.

No event collapses unrelated state machines into a generic platform status.

---

## 55. System Architecture Alignment

This contract preserves:

- transactional outbox;
- at-least-once dispatch;
- duplicate-tolerant consumers;
- non-authoritative realtime;
- no network call inside authoritative business transactions;
- worker failure affecting freshness rather than correctness.

---

## 56. Relational Schema Alignment

This review requires the schema to:

- keep outbox fact identity/payload immutable;
- stop treating outbox insertion sequence as a public replay cursor;
- distinguish outbox processing from per-webhook delivery;
- persist webhook endpoint/subscription/delivery state separately.

---

## 57. API Contract Alignment

This contract does not redefine synchronous Partner commands.

A webhook never replaces an API response.

A realtime stream event never changes Reservation ownership.

Lost synchronous responses continue to use idempotency/API recovery.

Asynchronous delivery is intentionally a separate contract boundary.

---

# PART XII — REVIEW FINDINGS & REMEDIATIONS

## 58. Review Finding: One Realtime Transport Should Not Be Forced Across All Consumers

**Rejected design:** require external Partner backends to maintain the same realtime browser channel used by TktSync React applications.

**Reason rejected:** Partner backend systems need durable retry/acknowledgement semantics, while browser UI needs low-latency best-effort invalidation.

**Resolution:** browser realtime and Partner webhooks are separate delivery mechanisms sourced from the same outbox facts.

---

## 59. Review Finding: Outbox Insert Sequence Is Not a Safe Public Replay Cursor

**Risk:** PostgreSQL sequences are allocated before transaction commit. Concurrent transactions can become visible out of sequence-number order.

**Resolution:** insertion sequence remains internal dispatch metadata. The MVP exposes no gap-free public cursor. Browser clients resynchronize; Partner webhook consumers use event-ID deduplication and authoritative API recovery.

---

## 60. Review Finding: Per-Seat Partner Webhooks Would Create Volume and Privacy Problems

**Resolution:** Partner webhooks focus on Partner-owned transaction and Event lifecycle facts. High-volume inventory state remains an availability-query concern. Coarse inventory invalidation may be introduced later without exposing another Partner's activity.

---

## 61. Review Finding: Webhook URL Configuration Creates an SSRF Boundary

**Resolution:** production webhook destinations are HTTPS-only, redirect following is disabled, and destination DNS/IP validation prevents internal/private network access.

---

## 62. Review Finding: Webhook `published_at` Cannot Mean Delivered Everywhere

One outbox fact may fan out to:

- multiple browser connections;
- multiple Partner endpoints;
- endpoints with independent retry state.

**Resolution:** outbox processing state means dispatcher fan-out has been established/attempted. Partner delivery state lives in dedicated delivery records.

---

# PART XIII — NON-NEGOTIABLE EVENT INVARIANTS

## 63. Event Contract Invariants

1. No external event is published before the underlying business fact commits.
2. Realtime/webhook delivery never creates or changes inventory ownership.
3. Event receipt is not stronger than authoritative API/database state.
4. Event non-receipt is not proof that a business operation failed.
5. Browser realtime is best-effort and resynchronizes after interruption.
6. Partner webhook delivery is at least once.
7. Consumers must tolerate duplicates.
8. Strict cross-event ordering is not guaranteed.
9. The internal outbox sequence is not a public gap-free cursor.
10. Event IDs remain stable across delivery retries.
11. Partner webhook payloads contain only Partner-authorized data.
12. Selection-session realtime never leaks another session's Reservation.
13. Scanner realtime never substitutes for authoritative scan validation.
14. Raw QR, Reservation, selection, Partner, or webhook secrets never appear in event payloads.
15. Outbox publication failure never rolls back authoritative business state.
16. Slow realtime consumers never delay authoritative transactions.
17. Webhook redirects are not followed.
18. Webhook destinations are protected against SSRF.
19. Manual webhook replay replays the same fact and does not recreate business state.
20. Any future replay/cursor design requires explicit review rather than reusing outbox insertion sequence.

---

## 64. Final Contract Summary

~~~text
             AUTHORITATIVE TRANSACTION
                      |
                      v
               POSTGRESQL COMMIT
                      |
                      v
                OUTBOX FACT
                      |
              OUTBOX DISPATCHER
               /             \
              /               \
             v                 v
     Browser Realtime     Partner Webhooks
       best-effort          at-least-once
          |                     |
          v                     v
  refresh API state      query API as needed
~~~

The governing principle remains:

> **Asynchronous events tell consumers that committed TktSync facts exist. Only authoritative API/database state tells consumers what rights currently exist.**

---

**End of Document**

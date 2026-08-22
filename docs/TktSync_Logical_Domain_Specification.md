# TktSync Logical Domain Specification

**Document status:** Governing Logical Domain Specification  
**Applies to:** TktSync MVP and compatible partner integrations  
**Version:** 1.0  
**Date:** 20 August 2026  
**Classification:** Confidential  
**Normative parent:** TktSync Platform Process & Policy Standard v1.0  
**Product basis:** TktSync Technical Brief (2026)

---

## 1. Purpose

This document defines the canonical logical domain model for TktSync. It translates the governing platform policy into explicit domain concepts, state machines, transition rules, ownership relationships, authorization boundaries, consistency requirements, and business invariants.

The specification intentionally does **not** prescribe a database schema, framework, deployment architecture, queueing technology, cache implementation, or HTTP route layout. Those implementation decisions are subordinate to this domain model and MUST preserve its semantics.

The primary purpose of the model is to guarantee that all implementations answer the following questions consistently:

- What inventory exists for an event?
- Which inventory is available to a particular sales channel?
- Who currently has rights over that inventory?
- What stage is a buyer transaction in?
- When may a transaction proceed to payment?
- When is a sale authoritative?
- What ticket entitlement exists after confirmation or non-public issuance?
- Which QR credential is valid for that entitlement?
- Has the entitlement already been admitted?
- Which actor may perform each transition?
- What happens when requests race, timeouts occur, or external payment status is uncertain?

---

## 2. Authority and Precedence

The following precedence governs domain interpretation:

1. **TktSync Platform Process & Policy Standard v1.0** — authoritative platform policy.
2. **This Logical Domain Specification** — authoritative translation of that policy into domain semantics.
3. **TktSync Technical Brief (2026)** — product scope and original product intent.
4. API specifications, database designs, service boundaries, UI flows, and implementation documents.

Where the original technical brief is intentionally extended by the approved platform policy, the approved policy and this specification take precedence. In particular, the protected-checkout and reconciliation lifecycle extends the brief's original Availability / Hold / Confirm-or-Release flow to prevent valid buyers from losing inventory while payment is in flight. This extension does not make TktSync a payment processor.

---

## 3. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

- **MUST / MUST NOT** define mandatory domain behavior.
- **SHOULD / SHOULD NOT** define expected domain behavior unless a documented exceptional case applies.
- **MAY** defines permitted behavior.

A lower-level implementation MUST NOT reinterpret a domain term to mean something materially different from this document.

---

## 4. Domain Modeling Principles

### 4.1 Separate Independent State Dimensions

TktSync MUST NOT use one overloaded status to represent unrelated domain concerns.

The following are independent state dimensions:

| Dimension | Canonical question |
|---|---|
| Event lifecycle | Is this event accepting new sales, paused, closed, completed, or cancelled? |
| Reservation lifecycle | What stage is this buyer's inventory acquisition in? |
| Reserved inventory disposition | What currently prevents or permits this named inventory unit from being acquired? |
| GA pool accounting | How is the pool capacity currently distributed? |
| Allocation/restriction lifecycle | Why is inventory withheld, and for whom? |
| Ticket entitlement | Does this ticket still grant an entitlement? |
| QR credential | Is this particular credential currently valid? |
| Admission | Has the entitlement been authoritatively admitted? |
| Partner account | May this partner initiate operations? |
| Partner credential | Is this credential authorized to authenticate requests? |

No implementation may collapse these dimensions if doing so changes their independent semantics.

### 4.2 Stable Business Identity

Domain identity MUST be independent from mutable display labels.

Examples:

- a seat's logical identity is not merely the string `A12`;
- a ticket's identity is not its QR payload;
- a partner's identity is not an API key;
- an event inventory unit's identity is not the current seat-map label.

Human-readable labels MAY change where policy permits presentation changes, but historical identity MUST remain stable.

### 4.3 Authoritative State vs Derived Views

Authoritative state is held by the domain objects defined in this specification.

The following are derived views and MUST NOT become alternate authorities:

- availability responses;
- seat-map projections;
- realtime messages;
- admin dashboards;
- partner reports;
- accreditation exports;
- cached counters;
- search indexes.

### 4.4 Logical Expiry Is Time-Derived

A delayed cleanup worker MUST NOT extend a buyer's rights beyond an authoritative deadline.

Before accepting a state-changing command, the domain MUST evaluate any relevant server-authoritative deadline and determine the effective current state even if an asynchronous cleanup task has not yet materialized the expiry transition.

### 4.5 Historical Facts Are Preserved

Confirmation, issuance, ticket voiding, credential rotation, admission, privileged overrides, and other material transitions MUST remain historically explainable.

A current-state change MUST NOT erase the fact that an earlier state or transaction existed.

---

## 5. Canonical Domain Vocabulary

### 5.1 Venue

A reusable physical layout identity.

A venue describes the physical structure of an event location but is not itself live event inventory.

### 5.2 Venue Layout Version

A versioned physical layout definition belonging to a venue.

It contains sections, zones, rows, seats, tables, GA zones, orientation information, and related presentation metadata.

### 5.3 Event

A commercial/event occurrence that references a venue layout and receives its own event-specific inventory, pricing, sale lifecycle, and admission policy.

### 5.4 Event Layout Snapshot

The event-specific layout reference/snapshot from which live event inventory is materialized.

It protects a live event from silent mutation when the reusable venue is later edited.

### 5.5 Reserved Inventory Unit

A named, individually addressable event-specific unit, normally a seat.

### 5.6 GA Inventory Pool

A **General Admission (GA) Inventory Pool** is a count-based event-specific inventory pool representing a general-admission zone.

### 5.7 Reservation

The authoritative buyer inventory-acquisition transaction.

A reservation owns one or more reservation items and moves through hold, protected checkout, payment retry, reconciliation, and terminal states.

### 5.8 Reservation Item

A reservation child representing either:

- one reserved inventory unit; or
- a quantity from one GA inventory pool.

A reservation item records the inventory source and the event-controlled commercial terms captured for that acquisition.

### 5.9 Checkout Attempt

A bounded protected-payment attempt belonging to a reservation.

It records the protection window and outcome without making TktSync responsible for payment processing.

### 5.10 Inventory Restriction

An event-owner-controlled withholding construct that removes inventory from the shared public pool.

It is either:

- a **Block** — inventory withheld from acquisition without a sales-channel ownership grant; or
- an **Allocation** — inventory assigned to a defined audience, purpose, or sales channel.

### 5.11 Sale

The immutable business fact that TktSync accepted commercial confirmation for a reservation.

A sale is created exactly once for a confirmed reservation.

### 5.12 Sale Item

The commercial confirmation record for inventory acquired by a sale.

### 5.13 Non-Public Issuance

The immutable business fact that an authorized allocation produced a ticket entitlement without a commercial sale.

Examples include a comp, sponsor, VIP, media, or team entitlement.

### 5.14 Ticket Entitlement

The stable admission entitlement created by either:

- a confirmed commercial sale; or
- an authorized non-public issuance.

### 5.15 QR Credential

A revocable credential that represents a ticket entitlement for admission.

The credential is not the ticket identity.

### 5.16 Scan Attempt

One authoritative validation request for a presented credential.

It records the validation outcome and supports request-level idempotency.

### 5.17 Admission

The authoritative fact that a ticket entitlement was admitted.

For the MVP single-entry policy, at most one active admission exists per ticket entitlement.

### 5.18 Audit Event

An append-only record of a material domain action or privileged decision.

### 5.19 Partner

An authenticated external ticketing channel.

### 5.20 Buyer Selection Session

A narrowly scoped customer-facing session used by the TktSync white-label selector to view inventory and acquire or modify a reservation on behalf of a partner.

It is not a general partner credential.

---

## 6. Bounded Domain Areas

The logical model is divided into the following domain areas.

### 6.1 Venue & Layout

Owns:

- Venue;
- VenueLayoutVersion;
- SectionDefinition;
- RowDefinition;
- SeatDefinition;
- TableDefinition;
- GAZoneDefinition;
- orientation and visual metadata.

### 6.2 Event & Inventory

Owns:

- Event;
- EventLayoutSnapshot;
- EventPriceTier;
- EventPriceAssignment;
- ReservedInventoryUnit;
- GAInventoryPool;
- sale and admission windows.

### 6.3 Reservation & Checkout Protection

Owns:

- Reservation;
- ReservationItem;
- CheckoutAttempt;
- hold deadlines;
- payment retry deadlines;
- reconciliation deadlines;
- commercial term snapshots.

### 6.4 Restrictions & Allocations

Owns:

- InventoryRestriction;
- Block;
- Allocation;
- allocation items/quantities;
- channel-specific and non-public allocation rules.

### 6.5 Ticketing & Entitlements

Owns:

- Sale;
- SaleItem;
- NonPublicIssuance;
- TicketEntitlement;
- QRCredential;
- ticket void and credential rotation behavior.

### 6.6 Admission

Owns:

- ScanAttempt;
- Admission;
- privileged admission correction/override;
- duplicate-admission prevention.

### 6.7 Partner & Authorization

Owns:

- Partner;
- PartnerCredential;
- PartnerEventAccess;
- event staff authorization assignments;
- buyer selection capability scope.

### 6.8 Audit, Reporting & Projections

Owns logical contracts for:

- AuditEvent;
- AvailabilityProjection;
- realtime inventory projection;
- PartnerReport;
- AccreditationExport.

These are derived from authoritative domain state.

---

## 7. High-Level Relationship Model

```text
Venue
  └── VenueLayoutVersion
        ├── SectionDefinition
        ├── RowDefinition
        ├── SeatDefinition
        ├── TableDefinition
        └── GAZoneDefinition

Event
  ├── references EventLayoutSnapshot
  ├── EventPriceTier / EventPriceAssignment
  ├── ReservedInventoryUnit*
  ├── GAInventoryPool*
  ├── InventoryRestriction*
  └── PartnerEventAccess*

Partner
  └── Reservation*
        ├── ReservationItem*
        └── CheckoutAttempt*
              │
              └── on confirmation
                    ↓
                  Sale
                    ├── SaleItem*
                    └── TicketEntitlement*
                          ├── QRCredential*
                          ├── ScanAttempt*
                          └── Admission*

Allocation
  └── may produce NonPublicIssuance
        └── TicketEntitlement*
              ├── QRCredential*
              ├── ScanAttempt*
              └── Admission*

All material transitions
  └── AuditEvent*
```

---

## 8. Venue and Layout Domain

This domain is the logical basis for the product's visual floor plan builder and the live buyer seat-map representation derived from it.

### 8.1 Venue

A Venue is a reusable identity containing descriptive location-level metadata.

A Venue MUST NOT directly own live sale state.

### 8.2 Venue Layout Version Lifecycle

Canonical states:

`DRAFT -> PUBLISHED -> RETIRED`

#### DRAFT

- MAY be edited freely while unreferenced by live inventory.
- MAY be discarded if it has never been used to create protected business history.

#### PUBLISHED

- is an immutable physical-layout baseline;
- MAY be selected when creating an event layout snapshot;
- MUST NOT be modified in-place in a way that changes existing physical identity.

Material edits to a published layout create a new version.

#### RETIRED

- cannot be selected for new events by default;
- remains historically readable;
- remains valid for events that already reference it.

### 8.3 Layout Components

A published layout MAY include:

- sections/zones;
- rows;
- named seats;
- tables as visual/grouping constructs;
- GA zones;
- stage, ring, field, or similar orientation metadata.

A table is a layout grouping in the MVP. Seats within a table MAY be individually reservable. Whole-table sale is not a required MVP domain capability.

### 8.4 Event Snapshotting

Creating event inventory from a venue MUST create or bind an event-specific layout snapshot.

Later changes to the reusable Venue or a newer VenueLayoutVersion MUST NOT silently mutate an existing event.

While an Event remains `DRAFT` and has no protected business history, event layout may be regenerated or edited according to administrative rules.

After an Event enters `ON_SALE`, physical inventory identity is frozen except for explicitly safe presentation changes.

---

## 9. Event Domain

### 9.1 Event Lifecycle States

Canonical states:

- `DRAFT`
- `ON_SALE`
- `PAUSED`
- `SALES_CLOSED`
- `COMPLETED`
- `CANCELLED`

### 9.2 Event State Machine

```text
DRAFT ───────────────→ ON_SALE ───────────────→ SALES_CLOSED ─────→ COMPLETED
  │                       │  ▲                         │
  │                       │  │                         │
  │                       ▼  │                         │
  │                     PAUSED ────────────────────────┘
  │                       │
  └───────────────────────┼────────────────────────────→ CANCELLED
                          │
ON_SALE ──────────────────┤
PAUSED ───────────────────┤
SALES_CLOSED ─────────────┘
```

`COMPLETED` and `CANCELLED` are terminal sale-lifecycle states.

### 9.3 Transition Table

| From | To | Primary guard |
|---|---|---|
| DRAFT | ON_SALE | Event inventory/configuration validation passes |
| DRAFT | CANCELLED | Authorized cancellation; no new sale activity |
| ON_SALE | PAUSED | Authorized event operation |
| PAUSED | ON_SALE | Authorized resume |
| ON_SALE | SALES_CLOSED | Authorized or scheduled sales close |
| PAUSED | SALES_CLOSED | Authorized or scheduled sales close |
| SALES_CLOSED | COMPLETED | Event operations are concluded |
| ON_SALE | CANCELLED | Authorized event cancellation |
| PAUSED | CANCELLED | Authorized event cancellation |
| SALES_CLOSED | CANCELLED | Authorized event cancellation |

### 9.4 State Semantics

#### DRAFT
- availability MUST NOT be published as purchasable inventory;
- new holds MUST NOT be accepted.

#### ON_SALE
- new holds MAY be accepted;
- partner and white-label selection MAY operate.

#### PAUSED
- new holds MUST be rejected;
- existing reservations MAY continue within already-authorized windows;
- inventory-expanding reservation modifications MUST NOT be accepted.

#### SALES_CLOSED
- new holds MUST be rejected;
- existing reservations MAY complete only within already-authorized windows;
- no expired transaction may be revived.

#### COMPLETED
- new commercial activity MUST be rejected;
- historical domain records remain readable.

#### CANCELLED
- new acquisition MUST stop immediately;
- ordinary `HELD` reservations MUST be terminated/released through cancellation cleanup;
- `PAYMENT_RETRY` reservations MUST be terminated because no payment is actively protected;
- `COMMITTING` reservations with potentially in-flight payment MUST enter reconciliation rather than being blindly released;
- existing `RECONCILING` reservations continue under cancellation-aware reconciliation;
- confirmed sales and tickets remain historical facts;
- ordinary admission MUST be denied.

### 9.5 Hard Deletion

An Event MAY be physically deleted only while it is `DRAFT` and has no protected business history.

Once an Event has entered `ON_SALE` or has produced reservations, restrictions, sales, tickets, scans, admissions, or material audit history, it MUST NOT be hard-deleted as a normal operation.

### 9.6 Sale Window vs Admission Window

Sale lifecycle and admission eligibility are separate concerns.

An Event SHOULD define:

- a sale window or explicit sale-state controls; and
- an admission window.

Ordinary admission requires:

- event not `DRAFT`;
- event not `CANCELLED`;
- event not `COMPLETED`;
- admission window currently permitting entry;
- valid ticket/credential and admission state.

Admission MAY occur while the event is `ON_SALE`, `PAUSED`, or `SALES_CLOSED` if the admission window permits it.

---

## 10. Pricing Domain

### 10.1 Event Pricing

Commercial pricing is event-specific.

A venue layout MAY contain default visual labels or reusable pricing hints, but the authoritative price for sale belongs to the Event.

### 10.2 Price Tier

An EventPriceTier defines an event-controlled commercial category.

It MAY be assigned to:

- a section;
- a reserved inventory unit;
- a GA pool;
- an allocation/channel rule where explicitly configured.

### 10.3 Commercial Terms Snapshot

When inventory is acquired into a ReservationItem, that item MUST snapshot the authoritative event-controlled commercial terms required to preserve the buyer's price.

The snapshot SHOULD include logically:

- price tier identity/label;
- unit amount;
- currency;
- any event-controlled price metadata needed to reproduce the transaction terms.

Partner service fees MUST NOT be included as TktSync event-controlled pricing.

### 10.4 Freeze Rule

Once a Reservation enters `COMMITTING`, its item set, quantities, and TktSync-controlled commercial terms are frozen.

Any material change after checkout protection begins requires a new explicitly authorized transaction or recovery workflow rather than silently modifying the protected order.

---

## 11. Reserved Inventory Domain

### 11.1 Canonical Dispositions

A ReservedInventoryUnit has exactly one current disposition:

- `AVAILABLE`
- `RESERVED`
- `BLOCKED`
- `ALLOCATED`
- `SOLD`
- `ISSUED`

`ISSUED` is distinct from `SOLD`.

- `SOLD` means a commercial Reservation was authoritatively confirmed.
- `ISSUED` means a ticket entitlement was created through a non-public allocation without a commercial sale.

This distinction is mandatory because platform policy defines `SOLD` as confirmed commercial sale.

### 11.2 Reserved Inventory State Machine

Shared commercial path:

```text
AVAILABLE -> RESERVED -> SOLD
                |
                +-- release / expiry -> AVAILABLE
```

Administrative withholding path:

```text
AVAILABLE <-> BLOCKED
AVAILABLE <-> ALLOCATED
BLOCKED   <-> ALLOCATED   (explicit safe reclassification)
```

Channel-allocation commercial path:

```text
ALLOCATED -> RESERVED -> SOLD
                |
                +-- release / expiry -> ALLOCATED
                    while source allocation remains active
```

Non-public issuance path:

```text
ALLOCATED -> ISSUED
```

Post-void inventory re-release is a separate explicit operation:

```text
SOLD / ISSUED
      |
      +-- TicketEntitlement voided
      +-- authorized re-release
              ↓
       AVAILABLE or eligible ALLOCATION
```

All transitions remain subject to source-disposition, allocation-lifecycle, Event-state, and authorization rules.

### 11.3 Valid Transitions

| From | To | Meaning |
|---|---|---|
| AVAILABLE | RESERVED | Shared-pool hold succeeds |
| AVAILABLE | BLOCKED | Administrative block succeeds |
| AVAILABLE | ALLOCATED | Administrative allocation succeeds |
| BLOCKED | AVAILABLE | Block released |
| BLOCKED | ALLOCATED | Explicit safe reclassification |
| ALLOCATED | BLOCKED | Explicit safe reclassification |
| ALLOCATED | AVAILABLE | Allocation released |
| ALLOCATED | RESERVED | Authorized channel allocation is held |
| ALLOCATED | ISSUED | Non-public allocation creates entitlement |
| RESERVED | SOLD | Commercial reservation confirms |
| RESERVED | AVAILABLE | Reservation from shared pool releases/expires |
| RESERVED | ALLOCATED | Reservation sourced from active channel allocation releases/expires |
| SOLD | AVAILABLE / ALLOCATED | Explicit post-void re-release only |
| ISSUED | AVAILABLE / ALLOCATED | Explicit post-void re-release only |

### 11.4 Source Disposition

When a ReservationItem acquires a reserved unit, it MUST record its acquisition source:

- shared availability; or
- a specific eligible allocation.

This source determines the normal restoration target if the reservation releases or expires.

A channel-allocated seat MUST NOT become general shared inventory merely because a buyer's hold expired while its source Allocation remains active.

If the source Allocation was explicitly released while the Reservation remained protected, the Reservation's later release/expiry follows that Allocation's recorded release destination. Administrative release therefore changes future restoration semantics without displacing the active buyer.

### 11.5 Current Claim Invariant

At any instant, a reserved unit may have at most one current claim:

- one active reservation; or
- one block; or
- one allocation; or
- one current commercial sold assignment; or
- one current non-public issued assignment.

Historical sales or issuances MAY coexist in history after void/re-release, but only one current entitlement path may consume the unit.

---

## 12. GA Inventory Domain

### 12.1 Pool Model

GA inventory is count-based and MUST NOT create synthetic seat records merely to mimic reserved seating.

A GAInventoryPool logically owns:

- total capacity;
- blocked quantity;
- unreserved allocated quantity;
- active reserved quantity;
- current commercially sold quantity;
- current non-public issued quantity;
- current available quantity.

### 12.2 Active Reserved Quantity

Active reserved quantity includes ReservationItems whose Reservation is effectively in:

- `HELD`;
- `COMMITTING`;
- `PAYMENT_RETRY`;
- `RECONCILING`.

### 12.3 Capacity Equation

At all times:

```text
capacity
=
available
+ blocked
+ allocated_unreserved
+ active_reserved
+ sold_current
+ issued_current
```

Therefore:

```text
available >= 0
```

MUST always hold.

### 12.4 Allocation-Sourced GA Reservations

A GA reservation MAY consume quantity from:

- the shared GA pool; or
- an allocation eligible for the requesting partner/audience.

When allocation-sourced GA is reserved:

- the available quantity within that allocation decreases;
- active reserved quantity increases.

On release/expiry:

- the quantity returns to the originating allocation if that allocation remains active;
- otherwise it follows the explicit allocation-release policy.

### 12.5 Historical Sales vs Current Capacity Consumption

A historical Sale remains immutable even if a ticket is later voided.

If voided GA inventory is explicitly re-released:

- current capacity consumption MAY decrease;
- the historical sale record remains unchanged.

Reporting MUST distinguish historical confirmed-sale facts from current capacity consumption where both are relevant.

---

## 13. Contextual Availability and Partner Neutrality

Availability is caller-contextual while inventory truth remains global.

### 13.1 Shared-Pool Neutrality

Shared `AVAILABLE` inventory MUST NOT carry hidden Partner priority.

When multiple eligible Partners contend for the same shared inventory, the authoritative atomic acquisition determines the winner. Preferential access is permitted only when represented explicitly through Event configuration such as a channel Allocation.

Ranking, API client identity, integration order, cache freshness, or commercial preference MUST NOT silently alter ownership rules for the shared pool.

### 13.2 Contextual Availability

A partner may be eligible to acquire:

- shared `AVAILABLE` inventory; and
- inventory assigned to that partner through an active channel allocation.

Another partner MUST NOT see that channel allocation as generally acquirable inventory.

Therefore an availability projection MAY differ by caller context without creating multiple sources of truth.

Caller-contextual availability MUST NOT expose another Partner's private buyer, hold, order, or payment-reference metadata merely because inventory is unavailable. A Partner may learn that inventory cannot be acquired without learning the private transaction that caused that state.

The underlying disposition, allocation identity, and acquisition guards remain authoritative.

---

## 14. Restriction and Allocation Domain

### 14.1 Restriction Types

Canonical restriction kinds:

- `BLOCK`
- `ALLOCATION`

### 14.2 Block

A Block removes inventory from acquisition without assigning it to a buyer or sales channel.

Typical categories include:

- production;
- house hold;
- operational reserve;
- security;
- other event-admin-defined reasons.

### 14.3 Allocation

An Allocation reserves inventory for a defined purpose or audience.

Canonical allocation purposes include:

- sponsor;
- VIP;
- media;
- complimentary;
- fighter/team;
- channel-specific partner allocation;
- other explicitly configured event allocation.

### 14.4 Allocation Audience

An Allocation MUST define its acquisition mode:

- `NON_PUBLIC` — eligible for authorized direct issuance, not normal partner sale;
- `CHANNEL` — eligible only to the assigned partner/channel's normal reservation and commercial confirmation flow.

### 14.5 Restriction Lifecycle

A logical InventoryRestriction has states:

- `ACTIVE`
- `RELEASED`
- `CLOSED`

`RELEASED` means remaining unconsumed inventory was returned/reclassified.

`CLOSED` means no further consumption or release is permitted because the restriction has been fully consumed, administratively finalized, or superseded.

### 14.6 Restriction Release and Existing Buyer Rights

Releasing or reclassifying an Allocation MUST NOT displace inventory already held by an active Reservation.

For an Allocation with active ReservationItems:

- already-reserved inventory remains bound to those Reservations until confirmation, release, or expiry;
- unreserved allocation inventory may be released/reclassified immediately;
- the Allocation MUST retain a release destination for inventory that later returns from an active Reservation;
- if the Reservation confirms, the inventory follows its normal commercial confirmation path;
- if it releases/expires after the Allocation is no longer active, the inventory returns to the Allocation's recorded release destination rather than to a nonexistent active allocation.

The default release destination is shared `AVAILABLE` unless an explicit safe reclassification specifies another destination.

### 14.7 Bulk Semantics

Creating or materially changing a multi-item restriction SHOULD be all-or-nothing by default.

A conflict with any protected buyer obligation SHOULD reject the whole requested mutation and identify conflicts.

Partial application MUST be an explicit mode, never an accidental result.

### 14.8 Non-Public Issuance

A `NON_PUBLIC` allocation MAY produce a NonPublicIssuance.

Issuance requires finalized event inventory identity and MUST NOT occur after the Event is `CANCELLED` or `COMPLETED`. If an implementation permits issuance before public sales open, the affected event inventory identity is treated as protected business history and may no longer be silently regenerated.

That operation:

1. validates allocation authority and remaining inventory;
2. consumes the selected allocated seat or quantity;
3. creates an immutable NonPublicIssuance record;
4. creates TicketEntitlement(s);
5. creates active QR credential(s);
6. transitions current inventory from `ALLOCATED` to `ISSUED` or adjusts GA allocation/issued quantities;
7. records audit history.

It MUST NOT create a Sale and MUST NOT be reported as commercial `SOLD`.

---

## 15. Reservation Domain

### 15.1 Reservation States

Canonical states:

- `HELD`
- `COMMITTING`
- `PAYMENT_RETRY`
- `RECONCILING`
- `CONFIRMED`
- `RELEASED`
- `EXPIRED`

`CONFIRMED`, `RELEASED`, and `EXPIRED` are terminal Reservation states.

The reason for release/expiry MAY record event cancellation, buyer abandonment, partner-declared failure, reconciliation timeout, administrative action, or another authorized cause without creating unnecessary terminal state variants.

### 15.2 Reservation State Machine

```text
                         ┌──────────────→ EXPIRED
                         │
                         │
HELD ────────────────────┼──────────────→ RELEASED
  │                      │
  │ begin checkout       │
  ▼                      │
COMMITTING ──────────────┼──────────────→ CONFIRMED
  │  │                   │
  │  │ definitive        │ unknown / protected-window expiry
  │  │ payment failure   ▼
  │  ▼              RECONCILING ───────→ CONFIRMED
  │ PAYMENT_RETRY          │   │
  │  │                     │   ├────────→ RELEASED
  │  │ new protected      │   └────────→ EXPIRED
  │  │ attempt            │
  │  └──────────────→ COMMITTING
  │
  └── safe explicit release ────────────→ RELEASED

PAYMENT_RETRY ───────────→ RELEASED / EXPIRED
```

### 15.3 Reservation Creation

A Reservation is created only when its complete requested inventory set can be acquired atomically.

There is no persistent `PENDING` reservation that partially owns inventory.

### 15.4 Multi-Item Atomicity

A reservation spanning:

- multiple reserved seats;
- GA quantity;
- or a mixture of reserved seats and GA

MUST either acquire the complete request or acquire none of it.

### 15.5 No Silent Substitution

The Reservation domain MUST NOT replace requested inventory without explicit customer-authorized selection or an explicitly invoked best-available rule.

### 15.6 Ownership Scope

A Reservation MUST identify:

- Event;
- owning Partner;
- buyer/session/order reference as appropriate;
- ReservationItems;
- authoritative deadlines;
- original creation time;
- maximum total reservation lifetime;
- terminal reason where applicable.

Only the owning partner, a valid scoped white-label capability, or an explicitly privileged administrative workflow may mutate it.

### 15.7 Modification Eligibility

Inventory composition MAY be modified only while Reservation state is `HELD`.

A successful modification MUST:

- preserve all-or-nothing semantics;
- preserve the existing Reservation if replacement acquisition fails;
- snapshot terms for newly acquired items;
- preserve bounded maximum reservation lifetime.

Once `COMMITTING` begins, item composition, quantities, source allocations, and TktSync-controlled commercial terms are frozen.

### 15.8 Effective-State Evaluation

Before any Reservation command is accepted, authoritative time guards MUST be evaluated.

Examples:

- an apparently persisted `HELD` reservation whose hold deadline has passed is effectively expired and cannot begin checkout;
- an apparently persisted `COMMITTING` reservation whose protection window ended is evaluated under reconciliation rules;
- a delayed worker cannot make an expired right valid.

### 15.9 Anti-Hoarding Constraints

Reservation rights are temporary purchase intent, not indefinite inventory ownership.

The domain MUST support bounded policy controls including:

- maximum quantity per Reservation;
- maximum active Reservations per Partner and/or buyer/session scope;
- maximum total Reservation lifetime;
- bounded CheckoutAttempt count or retry policy;
- bounded payment-retry duration;
- bounded reconciliation duration;
- acquisition rate limits and abuse controls.

No hold modification, retry, or checkout transition may reset the transaction in a way that defeats the maximum total Reservation lifetime.

---

## 16. Reservation Item Domain

Each ReservationItem MUST represent exactly one logical inventory selection.

### 16.1 Reserved Seat Item

Logical properties include:

- ReservedInventoryUnit reference;
- quantity = 1;
- acquisition source (`SHARED` or specific Allocation);
- event-controlled CommercialTermsSnapshot.

### 16.2 GA Item

Logical properties include:

- GAInventoryPool reference;
- quantity > 0;
- acquisition source (`SHARED` or specific Allocation);
- event-controlled CommercialTermsSnapshot.

### 16.3 Immutable Identity

ReservationItem identity remains stable across eligible hold modifications when the logical item remains unchanged.

Replacing one seat with another is a replacement of inventory selection, not a silent mutation of physical seat identity.

---

## 17. Checkout Attempt Domain

### 17.1 Purpose

A CheckoutAttempt records one bounded period during which TktSync has protected a frozen Reservation so that the owning partner may attempt payment.

TktSync does not store or process payment card data and does not become the payment authority.

### 17.2 Checkout Attempt States

Canonical logical outcomes:

- `ACTIVE`
- `PAYMENT_FAILED`
- `UNCERTAIN`
- `CONFIRMED`
- `ABANDONED`

Only one CheckoutAttempt MAY be `ACTIVE` for a Reservation at a time.

### 17.3 Begin Checkout

`HELD -> COMMITTING` creates the first active CheckoutAttempt.

`PAYMENT_RETRY -> COMMITTING` creates a new active CheckoutAttempt.

The command MUST:

- confirm Reservation ownership;
- confirm effective Reservation validity;
- freeze Reservation composition and commercial terms;
- create a bounded protection deadline;
- preserve the Reservation's maximum total lifetime constraints.

### 17.4 Definitive Payment Failure

When the owning partner authoritatively reports payment failure:

- current CheckoutAttempt becomes `PAYMENT_FAILED`;
- if retry policy permits, Reservation becomes `PAYMENT_RETRY`;
- otherwise Reservation may be safely released.

### 17.5 Uncertain Outcome

If payment outcome is unknown when the active protection window ends:

- current CheckoutAttempt becomes `UNCERTAIN`;
- Reservation becomes `RECONCILING`;
- inventory remains protected for the bounded reconciliation window.

### 17.6 Confirmation

When a valid commercial confirmation succeeds:

- current CheckoutAttempt becomes `CONFIRMED`;
- Reservation becomes `CONFIRMED`;
- Sale is created exactly once.

### 17.7 Safe Abandonment

If the partner can definitively establish that no chargeable payment succeeded and abandons the transaction:

- current CheckoutAttempt may become `ABANDONED`;
- Reservation may be released.

A partner MUST NOT use safe abandonment when payment status is uncertain.

---

## 18. Payment Retry and Reconciliation Semantics

### 18.1 Payment Retry

`PAYMENT_RETRY` means:

- the preceding payment attempt definitively failed;
- no payment is currently believed to be in flight;
- inventory remains temporarily protected to improve customer recovery;
- a new attempt may be started only within retry and total-lifetime limits.

Event cancellation MAY terminate a `PAYMENT_RETRY` reservation because there is no unresolved payment attempt requiring reconciliation.

### 18.2 Reconciliation

`RECONCILING` means:

- payment outcome is uncertain or completion is arriving late;
- inventory remains unavailable to other buyers;
- the partner may still confirm if valid;
- the partner may release only after it establishes that payment did not succeed;
- the state is bounded.

### 18.3 Reconciliation Expiry

At reconciliation expiry:

- if no confirmation exists and no authoritative proof of success has been accepted, Reservation becomes `EXPIRED`;
- inventory returns to its proper source disposition;
- a later confirmation MUST NOT silently reclaim that released inventory.

Any post-expiry recovery is a new explicit recovery workflow and must reacquire inventory atomically if fulfillment is still possible.

---

## 19. Event Cancellation Interaction With Reservations

Cancellation MUST NOT be modeled as a blind bulk update of every Reservation.

The authoritative Event state immediately changes command eligibility.

Default logical behavior:

| Reservation state at cancellation | Required behavior |
|---|---|
| HELD | Release/terminate with reason `EVENT_CANCELLED` |
| PAYMENT_RETRY | Release/terminate with reason `EVENT_CANCELLED` |
| COMMITTING | Enter cancellation-aware `RECONCILING` because payment may be in flight |
| RECONCILING | Continue bounded reconciliation with cancellation context |
| CONFIRMED | Preserve Sale/Ticket history; admission denied because Event is cancelled |
| RELEASED / EXPIRED | No change |

Once Event cancellation has authoritatively committed, no subsequent Reservation confirmation may create a new Sale.

If external payment succeeded but Event cancellation committed before TktSync accepted confirmation, the Partner remains responsible for refund or other financial remediation. TktSync records the Reservation's cancellation/reconciliation outcome and preserves audit history, but does not manufacture a commercial Sale after cancellation.

If TktSync accepted confirmation before Event cancellation committed, the Sale remains a valid historical fact. The subsequent Event cancellation makes the resulting TicketEntitlement non-admissible and the Partner remains responsible for customer financial remediation.

No cancellation path may silently re-sell inventory while payment status remains unresolved.

---

## 20. Partner Operational Disable and Credential Revocation

Partner operational state and credential validity are distinct.

### 20.1 Partner Account States

Canonical account states:

- `ACTIVE`
- `DISABLED`

`DISABLED` prevents new inventory acquisition.

By default, existing reservations created before operational disable MAY:

- be released;
- begin checkout within their existing hold window;
- complete confirmation/reconciliation within existing authorized windows.

Inventory-expanding modifications MUST NOT be permitted while the Partner is disabled.

### 20.2 Partner Credential States

Canonical credential states:

- `ACTIVE`
- `REVOKED`

A request authenticated with a revoked credential MUST fail authentication/authorization.

Credential revocation MUST NOT erase or automatically release existing Reservation rights. Another valid credential for the same Partner or an authorized recovery mechanism may continue eligible transactions.

---

## 21. Confirmation and Sale Domain

### 21.1 Confirmation Guard

Commercial confirmation is permitted only when:

- the caller is the owning Partner or an explicitly authorized recovery actor;
- Reservation is effectively `COMMITTING` within its accepted protection semantics or `RECONCILING` within reconciliation semantics;
- the transaction has not been fully released or expired;
- inventory remains bound to that Reservation;
- the idempotency identity is valid;
- event state permits the transaction to resolve under the policy rules.

### 21.2 Confirmation Outcome

A successful confirmation MUST be one logical atomic outcome:

1. Reservation becomes `CONFIRMED`;
2. each reservation inventory claim becomes commercially consumed;
3. reserved units transition `RESERVED -> SOLD`;
4. GA current sold quantity increases and active reserved quantity decreases;
5. exactly one Sale is created;
6. SaleItems are created from the frozen ReservationItems;
7. TicketEntitlements are created;
8. one active QR credential is established for each new TicketEntitlement;
9. authoritative audit facts are recorded.

An implementation MAY use multiple persistence mechanisms internally, but it MUST expose this as exactly one logical business outcome and must be recoverable idempotently.

### 21.3 Sale Immutability

A Sale is an immutable historical confirmation fact.

Ticket voiding, refunds, credential reissue, admission, or later inventory re-release MUST NOT rewrite the historical Sale into a state implying that confirmation never occurred.

### 21.4 Ticket Cardinality

For the MVP:

- one confirmed reserved seat produces one TicketEntitlement;
- GA quantity `N` produces `N` independently valid TicketEntitlements;
- a mixed Reservation produces the appropriate ticket count across all components.

TktSync MUST NOT represent four independently admissible GA tickets as one reusable QR unless a future explicit group-admission policy is introduced.

---

## 22. Non-Public Issuance Domain

A NonPublicIssuance is distinct from Sale.

### 22.1 Origin

It MUST originate from an eligible `NON_PUBLIC` Allocation.

### 22.2 Outcome

A successful issuance:

- consumes allocated inventory;
- creates one or more TicketEntitlements;
- creates active QR credentials;
- transitions reserved inventory to `ISSUED` or adjusts GA issued quantity;
- creates audit history.

### 22.3 Reporting

Non-public issuance MUST NOT be counted as commercial `SOLD`.

Reporting MAY expose a combined "committed/unavailable" total, but it MUST retain the ability to distinguish:

- commercial sold inventory;
- non-public issued inventory;
- blocked inventory;
- unconsumed allocated inventory;
- active reservations.

---

## 23. Ticket Entitlement Domain

### 23.1 Canonical States

- `ACTIVE`
- `VOIDED`

### 23.2 Entitlement Origin

Every TicketEntitlement MUST reference exactly one authoritative origin:

- a SaleItem; or
- a NonPublicIssuance item.

A reserved-seat TicketEntitlement references the specific ReservedInventoryUnit it consumes.

A GA TicketEntitlement references its GAInventoryPool and represents one independently admissible unit of confirmed/issued GA quantity. It does not create a synthetic pre-sale seat identity.

### 23.3 Ticket State Machine

`ACTIVE -> VOIDED`

`VOIDED` is terminal for that TicketEntitlement.

If replacement entitlement is required, a new TicketEntitlement is created through an explicit authorized workflow rather than rewriting the identity of the voided ticket.

### 23.4 Event Cancellation

Event cancellation does not require mass transition of every TicketEntitlement to `VOIDED`.

An `ACTIVE` ticket belonging to a `CANCELLED` Event fails ordinary admission because Event state is part of validation.

### 23.5 Void Semantics

Voiding a TicketEntitlement:

- invalidates its active admission credential(s);
- does not erase Sale or NonPublicIssuance history;
- does not automatically return inventory to availability;
- records an audit event.

### 23.6 Inventory Re-Release

An explicit re-release MAY occur only when:

- all current ticket entitlements consuming the relevant inventory have been voided or otherwise invalidated;
- no active reservation consumes the inventory;
- the actor is authorized;
- event state and re-release policy allow it.

The destination MAY be:

- shared `AVAILABLE`; or
- an eligible active `ALLOCATION`;

depending on event policy and source context.

---

## 24. QR Credential Domain

### 24.1 Canonical States

- `ACTIVE`
- `SUPERSEDED`
- `REVOKED`

`SUPERSEDED` and `REVOKED` are terminal credential states.

### 24.2 Credential Invariant

At most one QR Credential MAY be `ACTIVE` for a TicketEntitlement at a time.

### 24.3 Reissue

Credential reissue is one logical operation:

1. validate authority and ticket state;
2. transition current credential `ACTIVE -> SUPERSEDED`;
3. create replacement credential `ACTIVE`;
4. record audit history.

### 24.4 Revocation

Voiding a TicketEntitlement MUST revoke any active credential as part of the same logical business operation.

A compromised credential MAY be revoked/reissued without voiding the TicketEntitlement.

### 24.5 Credential Security Semantics

A QR payload SHOULD contain only the minimum information required to resolve/validate the credential and SHOULD NOT expose unnecessary customer PII.

Possession of decoded QR data MUST NOT grant authority to mutate ticket or inventory state.

---

## 25. Admission Domain

### 25.1 Scan Attempt vs Admission

A ScanAttempt and an Admission are distinct.

A credential can be scanned many times, but only an accepted admission creates an Admission record.

### 25.2 Admission States

Canonical Admission states:

- `ACTIVE`
- `REVERSED`

Normal admission creates an `ACTIVE` Admission.

Only an explicit privileged correction may transition:

`ACTIVE -> REVERSED`

A reversed Admission remains historical but no longer counts as the ticket's active admission for future eligibility.

### 25.3 MVP Admission Policy

The default MVP policy is `SINGLE_ENTRY`.

Under `SINGLE_ENTRY`, a TicketEntitlement may have at most one `ACTIVE` Admission.

### 25.4 Validation Inputs

Ordinary admission MUST evaluate:

- scanner authorization;
- Event identity;
- Event lifecycle;
- admission window;
- TicketEntitlement state;
- QR Credential state;
- credential-to-ticket binding;
- prior active Admission;
- any event-specific validation constraints.

### 25.5 Scan Attempt Outcomes

Stable logical outcomes SHOULD distinguish at least:

- `ADMITTED`
- `ALREADY_ADMITTED`
- `INVALID_CREDENTIAL`
- `CREDENTIAL_REVOKED`
- `TICKET_VOID`
- `WRONG_EVENT`
- `EVENT_CANCELLED`
- `ADMISSION_NOT_OPEN`
- `NOT_AUTHORIZED`
- `AUTHORITY_UNAVAILABLE`

### 25.6 Concurrent Scans

For distinct scan operations racing on the same single-entry TicketEntitlement:

- exactly one may create the active Admission;
- later distinct operations receive `ALREADY_ADMITTED`.

### 25.7 Idempotent Retry

Retrying the same ScanAttempt identity returns the original logical result.

If the original attempt admitted the ticket, a retry MUST return the original success rather than incorrectly presenting it as fraud/duplicate entry.

### 25.8 Supervisor Correction

A mistaken admission MUST NOT be deleted from history.

A Gate Supervisor or Platform Administrator MAY perform an explicit admission correction when authorized.

A correction:

- records the reason and actor;
- marks the previous Admission as reversed/inactive for future eligibility;
- preserves the original Admission and ScanAttempt history;
- MAY permit a subsequent legitimate admission under event policy.

This is a correction of current eligibility, not erasure of historical fact.

### 25.9 Manual Override

A privileged manual admission override:

- requires elevated authority;
- requires a reason;
- records the validation state that was overridden;
- creates an auditable Admission outcome;
- MUST NOT silently rewrite prior scan history.

A Gate Supervisor override is event-scoped and MUST NOT silently convert a wrong-event credential, a `VOIDED` TicketEntitlement, a revoked credential, or a `CANCELLED` Event into ordinary valid admission. Any exceptional platform-level recovery beyond those limits requires explicit Platform Administrator authority and audit evidence.

Under `SINGLE_ENTRY`, a manual override MUST NOT create a second `ACTIVE` Admission. If an earlier Admission is known to be erroneous, it must be corrected/reversed first or be reversed atomically as part of the same privileged correction workflow.

---

## 26. White-Label Buyer Selection Domain

The TktSync white-label selector operates as a narrowly scoped acquisition surface on behalf of a Partner.

A BuyerSelectionSession MUST be scoped to:

- Partner;
- Event;
- buyer/session reference;
- permitted selection/hold operations;
- expiry.

The session MAY:

- read caller-contextual availability;
- create a hold;
- modify its own `HELD` Reservation;
- release its own `HELD` Reservation.

It MUST NOT:

- act as a general partner API credential;
- confirm a sale;
- process payment;
- access unrelated Partner transactions;
- perform administrative inventory operations.

The buyer returns to the owning Partner's checkout with a transaction/hold continuation reference.

---

## 27. Responsibility Boundary and Customer Data Minimization

### 27.1 Payment Is External

TktSync MUST NOT model card authorization, capture, settlement, processor fees, or refund settlement as authoritative TktSync aggregates.

The Reservation domain may record only the partner-provided references and outcome assertions required to protect inventory and reconcile confirmation.

A partner-reported payment success is not itself a Sale; only accepted TktSync confirmation creates a Sale.

### 27.2 Customer Relationship Is External

The Partner remains the owner of customer relationship, checkout presentation, customer communications, partner branding, and partner service fees.

TktSync MUST NOT require unrelated customer profile data merely to hold inventory.

### 27.3 Customer References

Reservation and ticket operations SHOULD use partner-controlled opaque references where sufficient, including:

- partner customer reference;
- partner order reference;
- buyer/session reference.

Additional attendee/customer data MAY be stored only where needed for ticket issuance, accreditation, event operation, or an explicit compliance requirement.

### 27.4 PII in Credentials and Projections

QR Credentials, realtime payloads, logs, and inventory projections SHOULD contain the minimum PII required for their purpose.

Customer PII MUST NOT become part of inventory identity.

### 27.5 Credential and Capability Isolation

Administrative credentials and Partner-secret credentials MUST NOT be exposed to untrusted buyer-facing clients.

BuyerSelectionSession and Reservation continuation capabilities MUST be:

- narrowly scoped to the owning Partner/Event/transaction;
- limited to explicitly permitted operations;
- time-bounded where applicable;
- insufficient by themselves to grant unrelated Partner or administrative authority.

Knowledge of a Reservation identifier or hold token alone MUST NOT authorize unrestricted mutation.

### 27.6 Physical Event Operations Are External

Gate staffing, physical security, wristbands, physical queue management, VIP hosting, and comparable venue operations remain Event Owner responsibilities.

TktSync governs the digital validation decision and audit record; it does not become the authority for physical venue operations.

---

## 28. Actor and Role Model

### 28.1 Platform-Level Actors

- `PLATFORM_ADMIN`

### 28.2 Event Staff Roles

The authorization model MUST be capable of distinguishing at least:

- `EVENT_MANAGER`
- `BOX_OFFICE`
- `GATE_SUPERVISOR`
- `SCANNER`
- `VIEWER`

A deployment MAY initially grant multiple roles to the same person, but the logical model MUST NOT assume every event user is an omnipotent administrator.

### 28.3 External Machine/Session Actors

- `PARTNER`
- `BUYER_SELECTION_SESSION`

---

## 29. Command Authorization Matrix

Legend:

- **Y** — normally authorized, subject to object/event guards.
- **P** — privileged/conditional; stronger authorization and/or reason required.
- **O** — owning-scope only.
- **N** — not authorized.

| Command | Platform Admin | Event Manager | Box Office | Partner | Buyer Session | Gate Supervisor | Scanner | Viewer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Create/edit Venue draft | Y | Y | N | N | N | N | N | N |
| Publish Venue layout version | Y | Y | N | N | N | N | N | N |
| Create/configure Event draft | Y | Y | N | N | N | N | N | N |
| Open/Pause/Resume/Close sales | Y | Y | N | N | N | N | N | N |
| Cancel Event | Y | P | N | N | N | N | N | N |
| Configure event pricing | Y | Y | P | N | N | N | N | N |
| Block/allocate inventory | Y | Y | P | N | N | N | N | N |
| Create non-public issuance | Y | Y | Y | N | N | N | N | N |
| Query event inventory | Y | Y | Y | Scoped | Scoped | Scoped | N | Scoped read |
| Create Reservation | N* | N* | N* | O | O | N | N | N |
| Modify HELD Reservation | P | P | P | O | O | N | N | N |
| Begin checkout | N | N | N | O | N | N | N | N |
| Report payment failure | N | N | N | O | N | N | N | N |
| Release Reservation | P | P | P | O | O while HELD | N | N | N |
| Confirm commercial Sale | P recovery | N | N | O | N | N | N | N |
| Void Ticket | P | P | P | O | N | N | N | N |
| Reissue QR Credential | P | P | Y | O | N | N | N | N |
| Re-release voided inventory | P | Y | P | Conditional event policy | N | N | N | N |
| Validate/scan ticket | P | P | P | N | N | Y | Y | N |
| Correct/override admission | P | P | P | N | N | Y | N | N |
| Event reporting | Y | Y | Y | Own scope | N | Scoped | N | Scoped |
| Event audit log | Y | Y | Scoped | Own transaction subset | N | Scoped admission | N | Scoped read |
| Accreditation export | Y | Y | Y | N | N | N | N | N |
| Disable Partner account | Y | N | N | N | N | N | N | N |
| Revoke Partner credential | Y | N | N | N | N | N | N | N |

`N*` means administrators do not use the ordinary buyer-hold command as a normal workflow; privileged support/recovery commands may act on reservations explicitly and audibly.

Partner void/reissue operations are restricted to tickets originating from that Partner's commercial sales.

---

## 30. Partner Event Access

A Partner MUST have explicit event access before it may participate in an Event.

Logical PartnerEventAccess states:

- `ACTIVE`
- `DISABLED`

An `ACTIVE` Partner account with `DISABLED` access to a particular Event MUST NOT acquire new inventory from that Event.

By default, disabling PartnerEventAccess follows the same customer-protective graceful semantics as operational Partner disable:

- no new holds;
- no inventory-expanding hold modifications;
- existing valid Reservations may release, begin checkout, confirm, or reconcile only within their already-authorized windows.

A security incident requiring immediate authentication revocation is handled through credential revocation, not by silently destroying Reservation state.

Partner-wide operational disable supersedes event-level access.

---

## 31. Domain Commands

The following are logical commands, not prescribed HTTP endpoint names.

### 31.1 Venue/Event Commands

- `CreateVenue`
- `CreateVenueLayoutVersion`
- `PublishVenueLayoutVersion`
- `CreateEvent`
- `ConfigureEventInventory`
- `ConfigureEventPricing`
- `OpenSales`
- `PauseSales`
- `ResumeSales`
- `CloseSales`
- `CancelEvent`

### 31.2 Inventory Restriction Commands

- `CreateBlock`
- `ReleaseBlock`
- `CreateAllocation`
- `ReleaseAllocation`
- `ReclassifyRestriction`
- `CreateNonPublicIssuance`

### 31.3 Reservation Commands

- `QueryAvailability`
- `CreateReservationHold`
- `ModifyReservationHold`
- `BeginCheckout`
- `ReportPaymentFailure`
- `ReleaseReservation`
- `ConfirmReservation`
- `GetReservationStatus`

### 31.4 Ticket Commands

- `VoidTicket`
- `ReissueCredential`
- `ReReleaseInventory`

### 31.5 Admission Commands

- `ValidateAndAdmit`
- `CorrectAdmission`
- `ManualAdmissionOverride`

### 31.6 Partner Administration Commands

- `GrantPartnerEventAccess`
- `DisablePartnerEventAccess`
- `DisablePartner`
- `RevokePartnerCredential`

The protected-checkout commands are domain-level additions required by the governing policy even though the original technical brief summarized partner integration as three core flows.

---

## 32. State-Dependent Command Guards

### 32.1 Create Hold

Allowed only when:

- Event is `ON_SALE`;
- Partner and PartnerEventAccess permit acquisition;
- every requested inventory selection is eligible to the caller;
- all requested inventory can be acquired atomically;
- anti-hoarding and quantity policies pass.

### 32.2 Modify Hold

Allowed only when:

- Reservation is effectively `HELD`;
- owner/scope is valid;
- Event is `ON_SALE`, `PAUSED`, or `SALES_CLOSED`;
- when Event is `PAUSED` or `SALES_CLOSED`, only non-expanding changes that release inventory MAY be accepted; additions, swaps, or replacements that acquire new inventory are denied;
- when Event is `ON_SALE`, any replacement/addition still must acquire the complete requested new composition atomically;
- maximum lifetime is not reset.

### 32.3 Begin Checkout

Allowed when:

- Reservation is effectively `HELD` or `PAYMENT_RETRY`;
- owner Partner is valid;
- Event is `ON_SALE`, `PAUSED`, or `SALES_CLOSED`;
- Event is not `CANCELLED` or `COMPLETED`;
- Reservation has not exceeded applicable hold/retry/maximum lifetime;
- no active CheckoutAttempt already exists.

Operationally disabled Partner accounts MAY complete eligible pre-existing reservations under the policy's default graceful-disable semantics.

### 32.4 Confirm Reservation

Allowed when:

- Reservation is effectively `COMMITTING` or `RECONCILING`;
- owning Partner or authorized recovery actor is valid;
- idempotency rules pass;
- inventory remains bound to the Reservation;
- Event state allows resolution.

A `CANCELLED` Event does not permit new commercial confirmation. Ordering is authoritative: a confirmation accepted before cancellation remains historical; a confirmation arriving after cancellation is rejected and any external payment is remediated by the Partner.

### 32.5 Release Reservation

Allowed when:

- ownership/privilege is valid; and
- Reservation has not been confirmed; and
- payment outcome is known safe for release.

A `RECONCILING` Reservation MUST NOT be released merely to free inventory when payment outcome remains uncertain.

### 32.6 Void Ticket

Allowed when:

- TicketEntitlement is `ACTIVE`;
- actor owns or has privileged event authority;
- operation is idempotent;
- active credential is revoked as part of the logical outcome.

### 32.7 Re-Release Inventory

Allowed only after the consuming TicketEntitlement(s) have been voided and event/release policy permits the destination disposition.

### 32.8 Validate and Admit

Allowed only when:

- scanner/event authorization passes;
- TicketEntitlement is `ACTIVE`;
- QR Credential is `ACTIVE`;
- Event/admission window permits entry;
- no active Admission exists under single-entry policy.

---

## 33. Logical Consistency Boundaries

The following operations MUST behave atomically at the business level.

### 33.1 Reservation Acquisition Boundary

`CreateReservationHold` MUST atomically coordinate:

- Reservation creation;
- all ReservationItems;
- all reserved-unit claims;
- all GA quantity claims;
- allocation-source consumption;
- price snapshots;
- idempotency result.

No partial business success is permitted.

### 33.2 Reservation Modification Boundary

`ModifyReservationHold` MUST atomically:

- acquire replacement/additional inventory;
- preserve original inventory if replacement fails;
- release removed inventory only when the new composition is secured;
- update ReservationItems and snapshots;
- preserve lifetime constraints.

### 33.3 Release/Expiry Boundary

Release or expiry MUST atomically:

- transition Reservation terminal state;
- return each item to its correct source disposition;
- update GA quantities;
- prevent later ordinary confirmation.

### 33.4 Confirmation Boundary

Confirmation MUST produce one logical outcome spanning:

- Reservation;
- inventory consumption;
- Sale/SaleItems;
- TicketEntitlements;
- active QR Credentials;
- idempotency result;
- audit/domain facts.

### 33.5 Restriction Boundary

Default bulk block/allocation mutation MUST either apply to the complete requested selection or not apply.

### 33.6 Non-Public Issuance Boundary

Issuance MUST logically coordinate:

- allocation consumption;
- current inventory disposition;
- NonPublicIssuance;
- TicketEntitlements;
- QR Credentials;
- audit facts.

### 33.7 Admission Boundary

`ValidateAndAdmit` MUST atomically coordinate:

- credential/ticket validation;
- duplicate-admission check;
- creation of one active Admission;
- idempotent ScanAttempt result.

---

## 34. Race Adjudication Rules

### 34.1 Hold vs Hold

Two requests for the same eligible reserved unit cannot both succeed.

For GA, competing quantity acquisitions cannot collectively exceed eligible available quantity.

### 34.2 Hold vs Block/Allocation

Exactly one conflicting transition may acquire the inventory.

Administrative status does not retroactively defeat a buyer hold that already succeeded.

### 34.3 Confirm vs Release

Only one terminal Reservation transition may win.

If confirmation is authoritatively accepted first, later release returns the already-confirmed logical result/conflict and MUST NOT free inventory.

If a safe release is authoritatively accepted first, later ordinary confirmation fails.

### 34.4 Confirm vs Deadline

The accepted-before-deadline rule applies.

"Accepted" means the command has entered the authoritative domain mutation boundary, authenticated, and passed the relevant time/state guard. Mere arrival at a network edge does not by itself establish acceptance.

Once authoritatively accepted before the deadline, later internal completion time MUST NOT retroactively invalidate the command.

### 34.5 Scan vs Scan

Exactly one distinct valid single-entry admission may win.

### 34.6 Void vs Scan

- if admission commits first, the Admission remains historical even if the Ticket is subsequently voided;
- if ticket void commits first, subsequent ordinary scan is rejected;
- neither action erases the other action's historical fact.

### 34.7 Credential Reissue vs Scan

If the old credential is admitted before supersession commits, that Admission remains valid history and the ticket is already admitted.

If credential supersession commits first, the old credential is invalid for subsequent scan.

The replacement credential does not bypass prior admission state.

### 34.8 Event Cancellation vs Confirmation

If commercial confirmation is authoritatively accepted before Event cancellation commits:

- Reservation becomes `CONFIRMED`;
- Sale/Ticket history is created;
- the later Event cancellation makes ordinary admission invalid;
- Partner financial remediation remains external to TktSync.

If Event cancellation commits first:

- later commercial confirmation is rejected;
- protected transactions follow cancellation-aware reconciliation/termination;
- any external payment must be refunded or otherwise remediated by the Partner.

This ordering prevents post-cancellation Sale creation while preserving a Sale that already became authoritative.

---

## 35. Idempotency Contract

All externally retriable state-changing commands MUST support an operation identity.

Logical uniqueness is scoped by at least:

- caller/partner security scope;
- operation type;
- idempotency key.

### 35.1 Same Key, Same Intent

A retry with the same logical request MUST return the same logical outcome.

### 35.2 Same Key, Different Intent

The same idempotency identity with materially different request content MUST fail with `IDEMPOTENCY_CONFLICT`.

### 35.3 Stable Result

The stored/recoverable logical result SHOULD identify the created or affected business object so a lost network response does not create duplicate business effects.

---

## 36. Authoritative Time Contract

TktSync server-authoritative time governs:

- hold expiry;
- checkout protection;
- payment retry;
- reconciliation;
- sale windows;
- admission windows;
- scan/admission timestamp;
- audit timestamp.

Client clocks and countdowns are advisory display mechanisms.

A state cleanup worker MAY materialize time-based transitions asynchronously, but command guards MUST behave as though authoritative deadlines were applied on time.

---

## 37. Domain Events

Domain events are post-commit facts, not commands and not alternate authorities.

Representative logical facts include:

### Event
- `EventOpenedForSale`
- `EventPaused`
- `EventResumed`
- `EventSalesClosed`
- `EventCancelled`
- `EventCompleted`

### Reservation
- `ReservationHeld`
- `ReservationModified`
- `CheckoutProtectionStarted`
- `PaymentRetryOpened`
- `ReservationEnteredReconciliation`
- `ReservationConfirmed`
- `ReservationReleased`
- `ReservationExpired`

### Inventory Restriction
- `InventoryBlocked`
- `BlockReleased`
- `InventoryAllocated`
- `AllocationReleased`
- `NonPublicInventoryIssued`

### Ticketing
- `TicketIssued`
- `TicketVoided`
- `CredentialIssued`
- `CredentialSuperseded`
- `CredentialRevoked`

### Admission
- `AdmissionGranted`
- `AdmissionRejected`
- `AdmissionCorrected`
- `ManualAdmissionGranted`

Realtime feeds, dashboards, caches, and reports MAY subscribe to these facts after authoritative commit.

Publication delay MUST affect freshness only, not correctness.

---

## 38. Availability and Realtime Projections

### 38.1 Availability Projection

The availability projection SHOULD expose sufficient state for a caller to understand what it may currently attempt to acquire.

It MAY include:

- available reserved units;
- eligible channel-allocated units;
- GA available quantity;
- pricing display information;
- hold/realtime freshness metadata.

It MUST NOT imply that displayed inventory is guaranteed until hold succeeds.

### 38.2 Realtime Projection

Realtime updates SHOULD represent committed changes such as:

- inventory held;
- hold released/expired;
- inventory blocked/unblocked;
- allocation changes;
- sale confirmed.

Clients reconnecting after interruption MUST resynchronize from authoritative state.

---

## 39. Reporting Semantics

Reporting MUST distinguish business facts that are intentionally separate.

At minimum, the domain must preserve the ability to report:

- shared available inventory;
- active reserved inventory;
- blocked inventory;
- allocated but unconsumed inventory;
- commercial confirmed inventory (`SOLD`);
- non-public issued inventory (`ISSUED`);
- voided ticket entitlements;
- current capacity consumption;
- historical confirmed Sale quantities;
- admissions;
- rejected/duplicate scan attempts where required.

A comp/VIP/media issuance MUST NOT inflate commercial Sale counts.

An expired or released Reservation MUST NOT appear as sold.

A payment attempt outside TktSync MUST NOT appear as sold until TktSync confirmation exists.

### 39.1 Export Semantics

Accreditation and reporting exports are derived snapshots.

An export:

- MUST NOT mutate inventory, Reservation, Ticket, or Admission state;
- MUST NOT become an alternate source of truth;
- SHOULD include generation timestamp;
- SHOULD include sufficient Event context to identify the snapshot;
- MAY contain additional attendee/accreditation data only under the customer-data minimization rules.

---

## 40. Audit Domain

### 40.1 Append-Only Behavior

AuditEvent records are append-only historical facts.

### 40.2 Material Audit Coverage

Audit SHOULD cover at minimum:

- event lifecycle changes;
- inventory restriction changes;
- Reservation state transitions;
- commercial confirmation;
- non-public issuance;
- ticket void;
- credential rotation/revocation;
- admission and meaningful validation outcomes;
- admission corrections/overrides;
- partner disable/revocation;
- privileged recovery;
- inventory re-release.

### 40.3 Minimum Audit Context

Where applicable, a material AuditEvent SHOULD preserve:

- actor identity and actor type;
- Partner/Event scope;
- operation;
- affected business identity;
- previous state;
- new state;
- server-authoritative timestamp;
- Reservation/Sale/Ticket correlation references;
- idempotency identity where relevant;
- reason for privileged or exceptional action.

### 40.4 Privileged Reason Requirement

Privileged actions that override normal flow MUST include a reason and actor identity.

---

## 41. Failure and Degradation Semantics

### 41.1 Inventory Authority Unavailable

When authoritative inventory cannot be safely evaluated:

- new holds MUST fail closed;
- stale availability MUST NOT be converted into ownership;
- temporary inability to sell is preferable to overselling.

### 41.2 Realtime Unavailable

The system MAY continue authoritative transactions without realtime delivery if authoritative state remains available.

### 41.3 Expiry Processing Delayed

Effective-time guards prevent delayed workers from extending rights.

### 41.4 Confirmation Response Lost

Partner retries using the same idempotency identity or queries Reservation status.

### 41.5 Scan Authority Unavailable

The MVP cannot claim full duplicate-prevention guarantees without authoritative connectivity.

A privileged manual process MAY be used where operational policy permits and MUST remain auditable.

---

## 42. Business Error Semantics

The domain SHOULD preserve stable machine-readable outcomes equivalent to:

### Inventory / Reservation
- `INVENTORY_UNAVAILABLE`
- `INSUFFICIENT_GA_QUANTITY`
- `INVENTORY_NOT_ELIGIBLE_FOR_PARTNER`
- `HOLD_EXPIRED`
- `HOLD_NOT_OWNED`
- `RESERVATION_NOT_MODIFIABLE`
- `CHECKOUT_ALREADY_ACTIVE`
- `CHECKOUT_WINDOW_EXPIRED`
- `PAYMENT_STATUS_UNCERTAIN`
- `RECONCILIATION_EXPIRED`
- `ALREADY_CONFIRMED`
- `IDEMPOTENCY_CONFLICT`

### Event / Partner
- `EVENT_NOT_ON_SALE`
- `EVENT_PAUSED`
- `EVENT_SALES_CLOSED`
- `EVENT_CANCELLED`
- `PARTNER_DISABLED`
- `PARTNER_EVENT_ACCESS_DISABLED`
- `NOT_AUTHORIZED`

### Ticket / Admission
- `TICKET_INVALID`
- `TICKET_VOID`
- `CREDENTIAL_REVOKED`
- `CREDENTIAL_SUPERSEDED`
- `TICKET_ALREADY_ADMITTED`
- `ADMISSION_NOT_OPEN`
- `WRONG_EVENT`
- `AUTHORITY_TEMPORARILY_UNAVAILABLE`

Transport-layer status codes are implementation details and MUST NOT replace these business meanings.

---

## 43. Core Aggregate Roots and Ownership

The following logical aggregate roots are recommended because they own stable business identity and enforce local invariants.

### 43.1 Venue Aggregate
Root: `Venue`

Owns layout-version identity. A published VenueLayoutVersion is immutable.

### 43.2 Event Aggregate
Root: `Event`

Owns lifecycle, event-level configuration, layout snapshot reference, sale/admission policies, and pricing configuration.

Large inventory collections SHOULD NOT be treated as one in-memory aggregate merely because they belong to the same Event.

### 43.3 Reserved Inventory Unit Aggregate
Root: `ReservedInventoryUnit`

Owns current disposition and current-claim reference.

### 43.4 GA Inventory Pool Aggregate
Root: `GAInventoryPool`

Owns capacity accounting and pool-level quantity invariants.

### 43.5 Reservation Aggregate
Root: `Reservation`

Owns ReservationItems, CheckoutAttempts, deadlines, terminal reason, and transaction lifecycle.

### 43.6 Inventory Restriction Aggregate
Root: `InventoryRestriction`

Owns block/allocation identity, purpose, audience, remaining eligibility, and lifecycle.

### 43.7 Sale Aggregate
Root: `Sale`

Owns immutable commercial confirmation and SaleItems.

### 43.8 Non-Public Issuance Aggregate
Root: `NonPublicIssuance`

Owns immutable non-commercial entitlement issuance.

### 43.9 Ticket Aggregate
Root: `TicketEntitlement`

Owns current entitlement state, credential lifecycle, and admission eligibility relationship.

### 43.10 Partner Aggregate
Root: `Partner`

Owns Partner account state and partner-level identity.

Cross-aggregate business operations such as multi-seat hold and confirmation require explicit consistency coordination as defined in Section 33.

---

## 44. Domain Value Objects

The implementation SHOULD preserve the following logical value concepts even if physical representation varies.

- `Money` — amount + currency;
- `TimeWindow` — start/end with authoritative timezone/instant semantics;
- `CommercialTermsSnapshot`;
- `InventorySelection`;
- `AllocationPurpose`;
- `AllocationAudience`;
- `OperationIdentity`;
- `ActorContext`;
- `TerminalReason`;
- `AdmissionPolicy`;
- `PartnerOrderReference`;
- `BuyerSessionReference`.

---

## 45. Data Retention and Identity Preservation

Normal platform operation MUST NOT hard-delete business objects that are required to explain historical state.

At minimum, once material business history exists, the following remain logically retainable:

- Event;
- event inventory identity;
- Reservation;
- Sale;
- NonPublicIssuance;
- TicketEntitlement;
- credential history;
- ScanAttempt;
- Admission;
- AuditEvent;
- restriction/allocation history relevant to sold or issued inventory.

Retention duration is an implementation/compliance decision, but referential and audit meaning MUST not be broken by routine deletion.

---

## 46. MVP Domain Scope

The logical model directly supports the technical brief's MVP:

- one event and one venue;
- visual floor-plan configuration;
- GA, reserved seating, or mixed inventory;
- two to three partner integrations;
- realtime locking with hold timers;
- protected checkout and reconciliation required by the approved policy;
- white-label mobile-first seat selection;
- VIP/sponsor/comp allocation blocking;
- QR generation and validity;
- mobile-web scan validation;
- admin dashboard;
- audit log;
- partner reporting;
- accreditation export.

### 46.1 Intentionally Out of Scope

The logical model does not require implementation of:

- native mobile application;
- self-serve partner onboarding;
- payment processing;
- dynamic pricing;
- enterprise analytics;
- multilingual support;
- CRM or customer messaging;
- secondary ticket marketplace;
- ticket transfer;
- arbitrary workflow automation.

### 46.2 Accommodated but Not Necessarily Required for MVP UI

The domain model preserves clean semantics for:

- channel-specific allocations;
- non-public issuance;
- admission correction;
- explicit post-void inventory re-release;
- partner credential rotation.

These may be implemented minimally if the assessment does not require full administrative UX, but the underlying design MUST NOT make them semantically impossible or corrupt core reporting.

---

## 47. Non-Negotiable Logical Domain Invariants

1. TktSync is the sole authoritative inventory and validation domain.
2. Event lifecycle is separate from inventory, Reservation, Ticket, credential, and Admission state.
3. Venue layout identity is separate from event-specific inventory identity.
4. A live Event is insulated from silent later Venue edits.
5. Availability is a derived, caller-contextual read model and never ownership.
6. A reserved unit has exactly one current disposition.
7. A reserved unit has at most one current consuming claim.
8. GA capacity accounting always balances and available quantity never becomes negative.
9. Commercial `SOLD` and non-public `ISSUED` are semantically distinct.
10. Channel allocation does not become shared inventory merely because a buyer Reservation expires.
11. Multi-item Reservation acquisition is all-or-nothing.
12. Reservation inventory is never silently substituted.
13. Reservation composition may change only while `HELD`.
14. Entering `COMMITTING` freezes Reservation composition and TktSync-controlled commercial terms.
15. Protected checkout begins before chargeable partner payment.
16. Original hold expiry cannot dispossess a valid `COMMITTING` transaction.
17. Payment retry and reconciliation are bounded.
18. `RECONCILING` inventory is not publicly acquirable.
19. Late confirmation after authoritative reconciliation expiry cannot silently reclaim released inventory.
20. Confirmation creates exactly one immutable Sale.
21. Non-public issuance creates no Sale.
22. One reserved-seat commercial confirmation produces one TicketEntitlement for that seat.
23. GA quantity `N` produces `N` independently admissible TicketEntitlements in the MVP.
24. Ticket identity is separate from QR credential identity.
25. At most one QR credential is active per TicketEntitlement.
26. Ticket void does not erase Sale/Issuance history.
27. Ticket void does not automatically re-release inventory.
28. Event cancellation does not erase Sale/Ticket history.
29. Single-entry duplicate prevention is enforced on Admission, not by mutating Ticket into a generic `SCANNED` state.
30. A technical retry of the same ScanAttempt returns the same logical result.
31. A mistaken Admission is corrected explicitly; it is never deleted from history.
32. Partner operational disable and credential revocation are separate domain concepts.
33. An operationally disabled Partner cannot expand inventory ownership.
34. A revoked credential cannot authenticate, but revocation alone does not erase existing Reservation rights.
35. All externally retriable mutations are idempotent.
36. Same idempotency identity with different intent is rejected.
37. Logical expiry is enforced by authoritative time even when cleanup workers are delayed.
38. Realtime delivery affects freshness, not correctness.
39. Reports and exports are projections, not authorities.
40. Administrative power cannot silently overwrite active buyer obligations.
41. Privileged exceptions require explicit authority, reason, and audit history.
42. Historical business identity survives current-state changes.
43. When authoritative state is unsafe or ambiguous, new irreversible ownership changes fail closed rather than infer state from stale information.
44. Shared-pool Partner acquisition is neutral unless an explicit Event allocation states otherwise.
45. Buyer-facing clients never receive unrestricted Partner or administrative credentials.
46. Every implementation-level service, worker, API, scanner, UI, and administrative tool must preserve these invariants.

---

## 48. Normative Domain Clarifications

This section records domain-level resolutions required to keep the model internally consistent with the governing policy.

### 48.1 `ISSUED` Is Separate From `SOLD`

A comp, sponsor, VIP, media, or similar allocation can create a ticket without a commercial sale. Because the governing policy defines `SOLD` as confirmed commercial sale, non-public issuance uses `ISSUED` inventory semantics and a NonPublicIssuance record rather than fabricating a zero-value Sale.

### 48.2 Reservation Release Restores the Correct Source

A Reservation may originate from shared availability or a channel allocation. Release/expiry returns inventory to its original eligible source rather than always returning it to the shared pool.

### 48.3 Reservation Composition Freezes at Protected Checkout

Reservation composition changes are permitted only while `HELD`. Transition to `COMMITTING` freezes inventory selections, quantities, allocation source, and TktSync-controlled commercial terms so payment cannot proceed against a changing order.

### 48.4 Event Cancellation Uses Guard Dominance, Not Massive Synchronous Mutation

Changing Event to `CANCELLED` immediately changes what commands are legal. Ordinary holds can be cleaned up asynchronously, while potentially in-flight payments reconcile individually. The model therefore does not require one unbounded transaction updating every Reservation and seat.

### 48.5 Sale Is Historical; Ticket Entitlement Is Current

A Sale remains the immutable fact that confirmation occurred. Ticket voiding changes entitlement, not history. This prevents refund/void workflows from rewriting the platform's commercial record.

### 48.6 Admission Is Separate From Scan Attempt

A failed or duplicate scan is not an Admission. This distinction preserves accurate gate analytics and enables idempotent scanner retries.

### 48.7 Admission Correction Preserves History

Operational mistakes at the gate require a safe recovery path. Corrections reverse current admission eligibility explicitly rather than deleting the original Admission, preserving the governing audit policy.

### 48.8 Partner Disable Is Separate From Credential Revocation

Operational suspension controls whether a Partner may initiate new business, while credential revocation controls authentication. Conflating the two would either destroy legitimate in-flight buyer rights or fail to respond correctly to credential compromise.

### 48.9 Venue Versioning Protects Live Events

A reusable floor plan may evolve, but an event already selling inventory must retain stable physical identity. Published layout versions and event snapshots prevent silent mutation.

### 48.10 Availability Is Caller-Contextual

Explicit channel allocations mean two partners can legitimately have different acquirable views of the same authoritative inventory. This is not multiple inventory truth; it is one truth evaluated under different authorization/allocation contexts.

### 48.11 GA Does Not Use Synthetic Seats

GA remains a count-based domain. Individual TicketEntitlements are created only after confirmed sale/issuance for admission purposes; synthetic pre-sale seat records are unnecessary and would distort the inventory model.

### 48.12 Event Sale State and Admission Window Are Separate

Gate validation must not depend on whether new ticket sales are currently open. Admission eligibility is therefore governed by Event state plus a separate admission window/policy.

---

## 49. Configuration Parameters

The following are configuration values, not unresolved domain semantics:

- ordinary hold duration;
- checkout protection duration;
- payment retry duration;
- reconciliation grace duration;
- maximum total Reservation lifetime;
- maximum hold quantity;
- maximum active Reservations;
- rate limits;
- event sale start/end controls;
- admission open/close window;
- allocation categories;
- whether voided inventory may be re-released;
- whether an owning Partner may request re-release under event policy;
- future re-entry policy.

Configuration MUST remain bounded and MUST NOT defeat the invariants in this specification.

---

## 50. Implementation Handoff Requirements

Any subsequent database, API, service, or deployment design MUST explicitly demonstrate how it preserves:

1. reservation multi-item atomicity;
2. GA non-negative capacity;
3. source-aware reservation release;
4. state/time guards independent of cleanup-worker timing;
5. confirmation idempotency and exactly-one Sale creation;
6. ticket/credential separation;
7. scan concurrency and idempotency;
8. append-only auditability;
9. caller-contextual availability without duplicated truth;
10. event cancellation behavior;
11. partner isolation and credential revocation;
12. realtime projection consistency;
13. failure-closed behavior when authoritative inventory is unavailable.

No implementation specification should be approved if it cannot map each of these requirements to an enforcement mechanism.

---

## 51. Final Domain Summary

The canonical TktSync logical flow is:

```text
VENUE LAYOUT
    ↓ version/snapshot
EVENT
    ↓ materialize
EVENT INVENTORY
    ↓ acquire atomically
RESERVATION (HELD)
    ↓ protect before payment
RESERVATION (COMMITTING)
    ↓ confirm
SALE
    ↓ create entitlement
TICKET
    ↓ represented by
QR CREDENTIAL
    ↓ validate
SCAN ATTEMPT
    ↓ if eligible
ADMISSION
```

Alternative inventory paths remain explicit:

```text
AVAILABLE → BLOCKED → AVAILABLE
AVAILABLE → ALLOCATED → AVAILABLE
ALLOCATED (channel) → RESERVED → SOLD
ALLOCATED (non-public) → ISSUED
SOLD / ISSUED → [ticket void] → [explicit re-release] → AVAILABLE or eligible ALLOCATION
```

Uncertain payment remains protected:

```text
HELD
  ↓
COMMITTING
  ├── CONFIRMED
  ├── PAYMENT_RETRY → COMMITTING
  └── RECONCILING
         ├── CONFIRMED
         ├── RELEASED
         └── EXPIRED
```

These state dimensions remain independent. Their coordination—not status-field consolidation—is the basis of TktSync's correctness.

---

**End of Document**

# TktSync Platform Process & Policy Standard

**Document status:** Governing Platform Policy  
**Applies to:** TktSync MVP and all compatible partner integrations  
**Version:** 1.0  
**Date:** 20 August 2026  
**Classification:** Confidential

---

## 1. Purpose

This document defines the authoritative operating processes, policy rules, state semantics, responsibility boundaries, and platform invariants for TktSync.

TktSync is neutral ticket inventory infrastructure that enables multiple independent ticketing platforms to sell from one shared event inventory without overselling. TktSync is the single authority for inventory ownership and ticket validation. Ticketing partners retain responsibility for checkout, payment, customer relationships, platform branding, service fees, and customer communications. Event owners retain responsibility for physical event operations.

This document governs platform behavior. Implementation details—including database technology, queueing mechanisms, cache technology, transport protocols, and user-interface frameworks—must preserve the policies and invariants defined here.

---

## 2. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

- **MUST / MUST NOT** indicate mandatory platform behavior.
- **SHOULD / SHOULD NOT** indicate expected behavior unless a documented exceptional condition exists.
- **MAY** indicates permitted behavior.

Where implementation convenience conflicts with a policy in this document, the policy takes precedence.

---

## 3. Responsibility and Authority Boundaries

### 3.1 TktSync Authority

TktSync owns and authoritatively determines:

- live event inventory;
- GA inventory counts;
- reserved-seat state;
- seat and ticket holds;
- checkout protection state;
- sale confirmation;
- inventory blocking and allocation state;
- ticket identity;
- QR credentials and validity;
- duplicate-scan prevention;
- ticket validation;
- inventory audit history;
- partner inventory reporting;
- accreditation exports derived from authoritative records.

No partner, client, cache, realtime message, seat-map display, export, or administrative report may supersede TktSync's authoritative transaction state.

### 3.2 Ticketing Partner Authority

Ticketing partners own:

- buyer checkout;
- payment initiation and processing;
- payment-provider interaction;
- refunds and financial settlement;
- customer relationship;
- customer communications;
- partner branding;
- partner service fees.

A successful payment outside TktSync does not, by itself, create a ticket. A ticket sale becomes authoritative only when TktSync accepts the corresponding sale confirmation under the rules in this document.

### 3.3 Event Owner Authority

Event owners own:

- venue and event configuration;
- event pricing configuration;
- allocation and block configuration;
- gate staffing;
- physical security;
- wristbands;
- queue management;
- VIP hosting;
- other physical event operations.

Event-owner authority does not permit silent violation of active customer holds, confirmed sales, audit history, or inventory invariants.

---

## 4. Platform Actors

The platform recognizes the following logical actors:

### 4.1 Platform Administrator
A privileged TktSync operator responsible for platform-level administration, incident handling, partner credential management, and exceptional recovery actions.

### 4.2 Event Owner / Event Administrator
An authorized operator for one or more events who may configure venues, event inventory, pricing, allocations, blocks, sale state, reporting, and event-scoped operational settings.

### 4.3 Ticketing Partner
An authenticated external sales channel that may query inventory, acquire holds, begin checkout, confirm or release its own transactions, and retrieve its authorized ticketing results.

### 4.4 Scanner Operator
An authenticated event-scoped operator authorized to validate admission credentials.

### 4.5 Buyer Session
A customer-facing session operating through a ticketing partner or TktSync white-label selection interface. A buyer session never receives unrestricted administrative authority over inventory.

---

## 5. Governing Platform Principles

### 5.1 One Inventory Truth
TktSync MUST be the sole authority for whether inventory is available, held, protected for checkout, blocked, allocated, sold, voided, or otherwise unavailable.

### 5.2 One Validation Authority
TktSync MUST be the authority for whether a ticket credential is valid for admission and whether a single-entry ticket has already been admitted.

### 5.3 Availability Is Not Ownership
Availability responses are informational snapshots and MAY be cached. A displayed seat or GA quantity is not reserved until an authoritative hold succeeds.

### 5.4 Acquisition Is Strongly Consistent
Any operation that grants or changes inventory ownership MUST be validated against authoritative state and MUST preserve all inventory invariants atomically.

### 5.5 Customer Rights Survive Infrastructure Timing
A customer who has validly transitioned into protected checkout MUST NOT lose inventory merely because the original browsing hold expires while payment infrastructure is completing an accepted transaction.

### 5.6 Ambiguity Favors Safety
When authoritative state cannot be determined safely, TktSync MUST prefer temporary unavailability over overselling, duplicate issuance, destruction of legitimate buyer rights, or contradictory validation results.

### 5.7 Administrative Authority Does Not Bypass Integrity
Administrative operations MUST obey the same inventory integrity rules as partner operations unless an explicit privileged override policy applies. Exceptional overrides MUST be reasoned and auditable.

---

## 6. Authoritative Inventory Model

### 6.1 Venue Definition vs Event Inventory

A venue defines reusable physical structure, including:

- sections and zones;
- rows and seats;
- tables;
- GA zones and capacities;
- stage, ring, or field orientation;
- descriptive layout metadata.

An event MUST receive its own event-specific inventory derived from a venue configuration.

A venue seat answers:

> Does this physical seat exist?

An event inventory unit answers:

> For this event, what is the authoritative commercial and availability state of this seat or quantity?

Existing event inventory MUST NOT be silently rewritten when the reusable venue template changes.

### 6.2 Reserved Seating

Each reserved seat MUST have one authoritative event-specific inventory identity and one authoritative state at a time.

No reserved seat may belong simultaneously to:

- more than one active hold;
- an active hold and a block;
- more than one confirmed sale;
- a confirmed sale and normal public availability.

### 6.3 General Admission

GA inventory is count-based.

At all times:

`capacity >= sold + active holds + checkout-protected quantity + blocked/allocated quantity`

Available GA inventory MUST never become negative.

### 6.4 Mixed Inventory

An event MAY contain both reserved seating and GA inventory.

A transaction containing multiple inventory types MUST preserve all applicable invariants across the entire requested set.

---

## 7. Event Lifecycle Policy

An event MUST have a sale lifecycle separate from individual inventory-unit state.

The logical lifecycle is:

`DRAFT -> ON_SALE -> PAUSED / SALES_CLOSED -> COMPLETED`

An event MAY also transition to `CANCELLED` where applicable.

### 7.1 DRAFT
- New public holds MUST NOT be accepted.
- Event configuration MAY be changed subject to inventory-history constraints.

### 7.2 ON_SALE
- New holds MAY be accepted.
- Availability MAY be published.
- Partner and white-label sales flows MAY operate.

### 7.3 PAUSED
- New holds MUST be rejected.
- Existing valid holds remain valid until their existing deadlines.
- Existing protected checkout and reconciliation transactions MUST be allowed to resolve under their existing rules.
- Confirmed sales remain valid.

### 7.4 SALES_CLOSED
- New holds MUST be rejected.
- Existing in-flight transactions MAY complete only within their already-authorized windows.
- No new checkout extension may be created after the applicable transaction has fully expired.

### 7.5 COMPLETED
- New sales activity MUST be disabled.
- Historical sales, scans, audits, and reports MUST remain queryable according to retention policy.

### 7.6 CANCELLED
- New holds MUST stop immediately.
- Ordinary uncommitted holds SHOULD be terminated.
- Protected checkout transactions MUST enter an explicit cancellation/reconciliation path rather than being silently sold to another buyer.
- Confirmed tickets MUST remain historically recorded but MUST NOT validate for ordinary admission to the cancelled event.
- Refunds and financial customer remediation remain the ticketing partner's responsibility.
- Event cancellation MUST NOT erase sale or audit history.

---

## 8. Availability Process and Policy

### 8.1 Availability Query

Partners and customer-facing interfaces MAY request current availability.

Availability responses MAY be cached for performance.

### 8.2 No Reservation by Read

Reading or displaying availability MUST NOT create, extend, or imply ownership.

### 8.3 Stale Availability Is a Normal Business Condition

If an item displayed as available has been acquired by another buyer before the hold transaction commits, the hold MUST fail cleanly.

Customer-facing applications SHOULD:

1. explain that the inventory has just become unavailable;
2. refresh authoritative availability;
3. preserve the buyer's ability to select an alternative.

A concurrency conflict is not necessarily a platform failure.

### 8.4 Realtime Updates

Realtime messages MAY improve freshness, but MUST NOT establish ownership.

After reconnecting from a realtime interruption, clients SHOULD resynchronize from authoritative state instead of assuming that every intermediate event was received.

---

## 9. Hold Process and Policy

### 9.1 Hold Creation

A hold is the only normal mechanism by which available inventory becomes temporarily reserved for a buyer transaction.

A successful hold MUST:

- be created atomically;
- identify the owning partner;
- identify the applicable buyer/session/order reference;
- identify the event;
- identify all inventory included;
- record authoritative expiry;
- snapshot the applicable event-controlled commercial terms;
- return an opaque hold identifier or token suitable for continuation of the same transaction.

### 9.2 Atomic Multi-Item Holds

A hold request containing multiple items MUST be all-or-nothing.

If a buyer requests four seats and one cannot be acquired, TktSync MUST NOT silently hold the remaining three.

If a buyer requests four GA tickets and only three remain, TktSync MUST NOT silently reduce the quantity to three.

A partial alternative MAY be offered to the customer, but customer consent is required before a different request is executed.

### 9.3 No Silent Substitution

TktSync MUST NOT silently substitute one inventory unit for another.

A requested seat that is no longer available MAY trigger an alternative-selection experience, but the replacement inventory MUST be explicitly selected or accepted by the customer or by an explicitly invoked best-available policy.

### 9.4 Hold Ownership

Only the owning authorized partner or a privileged, explicitly authorized administrative process may:

- modify a hold;
- begin checkout for a hold;
- release a hold;
- confirm a hold.

Knowledge of a hold identifier alone MUST NOT grant unrestricted authority.

### 9.5 Hold Expiry

A normal hold is temporary.

If a hold has not entered an authorized protected checkout state by its server-authoritative deadline, it MAY expire and release its inventory.

Client clocks and displayed countdowns are advisory. TktSync server time is authoritative.

### 9.6 Anti-Hoarding

Holds represent genuine short-lived purchase intent and MUST NOT be usable as indefinite inventory reservations.

TktSync MAY enforce configurable controls including:

- maximum quantity per hold;
- maximum active holds per partner or buyer/session;
- maximum total reservation age;
- bounded checkout extensions;
- bounded retry windows;
- partner rate limits;
- abuse detection.

Repeatedly refreshing or modifying a hold MUST NOT create unlimited reservation life.

---

## 10. Hold Modification Policy

A hold MAY be modified only while its state permits modification.

### 10.1 Atomic Modification

A modification that replaces currently held inventory MUST be atomic from the customer's perspective.

If a customer holds `A10` and `A11` and requests `A12` and `A13`:

- if the new inventory can be acquired, the transition MAY complete;
- if it cannot be acquired, the original valid hold MUST remain intact unless the customer explicitly releases it.

### 10.2 Expiry Preservation

Adding, removing, or swapping inventory MUST NOT automatically reset the reservation to a fresh full hold duration.

The platform MUST preserve bounded reservation lifetime and anti-hoarding rules.

### 10.3 Commercial Terms

Newly added inventory MUST snapshot the commercial terms applicable when that inventory is successfully added.

Existing held inventory MUST retain the commercial terms already captured for that transaction unless a customer-authorized repricing process explicitly replaces the transaction.

---

## 11. Checkout Protection Policy

### 11.1 Protected Checkout Transition

Before a ticketing partner initiates an irreversible or potentially chargeable payment attempt, it MUST request checkout protection from TktSync.

The logical transition is:

`HELD -> COMMITTING`

TktSync MUST atomically verify that the hold is still valid before granting this transition.

### 11.2 Payment Must Follow Protection

A partner MUST NOT intentionally charge a customer first and only afterward attempt to secure or extend the inventory.

The required order is:

1. buyer commits to purchase;
2. partner requests protected checkout;
3. TktSync confirms protection;
4. partner initiates payment;
5. partner confirms or releases the transaction.

### 11.3 Protection Window

`COMMITTING` provides a bounded additional period for payment completion.

The original `HELD` deadline no longer releases the inventory once TktSync has successfully accepted the `COMMITTING` transition.

The protection period MUST remain bounded and MUST NOT be indefinitely renewable.

### 11.4 Customer Experience

Once checkout protection has been granted, customer-facing interfaces SHOULD stop presenting the original reservation countdown as though inventory can disappear during normal payment processing.

The customer SHOULD instead receive a processing state indicating that the inventory is secured while the accepted transaction is being completed.

---

## 12. Payment Failure, Retry, and Reconciliation Policy

TktSync does not process payment and therefore MUST NOT infer payment success solely from elapsed time.

### 12.1 Definitive Payment Failure

Where the partner receives a definitive payment failure and the platform policy permits retry, the transaction MAY enter a bounded payment-retry window.

The customer MAY be allowed to attempt another payment method without immediately losing inventory.

Payment retries MUST NOT create unlimited hold extensions.

### 12.2 Payment Success

After successful payment, the partner MUST confirm the transaction with TktSync using the original transaction identity and required idempotency information.

### 12.3 Unknown Payment Outcome

If the protected checkout window expires without a definitive confirm or release, the transaction MUST NOT immediately become public inventory.

It enters a bounded reconciliation state:

`COMMITTING -> RECONCILING`

During reconciliation:

- the inventory remains unavailable to other buyers;
- the partner MAY complete a valid delayed confirmation;
- the partner MAY explicitly release the transaction if it establishes that payment did not succeed;
- the state remains time-bounded.

### 12.4 Reconciliation Expiry

If reconciliation ends without valid confirmation, the inventory MAY be released.

After authoritative reconciliation expiry and release, a late confirmation MUST NOT silently reclaim inventory from the public pool.

Any post-reconciliation recovery MUST be an explicit recovery workflow that reacquires inventory atomically if permitted and records the exceptional action. If the original inventory is no longer available, the partner remains responsible for financial/customer compensation such as refund or an explicitly customer-approved alternative.

### 12.5 Boundary-Time Semantics

A transaction request accepted by TktSync while its authorization window is valid MUST be allowed to complete according to that accepted operation.

A customer MUST NOT fail solely because internal database or scheduler completion occurred milliseconds after the displayed deadline when TktSync had already authoritatively accepted the operation.

---

## 13. Confirmation Policy

### 13.1 Sale Authority

Only TktSync sale confirmation converts protected inventory into a confirmed sale.

Logical transition:

`COMMITTING / RECONCILING -> SOLD`

### 13.2 Confirmation Requirements

Confirmation MUST:

- reference the correct partner-owned transaction;
- preserve inventory integrity;
- be idempotent;
- create exactly one logical sale;
- create stable ticket identity for the sold inventory;
- preserve the snapshotted event-controlled commercial terms;
- record an audit event.

### 13.3 Retry Safety

If a confirmation response is lost, retrying the same logical request MUST return the same logical outcome and MUST NOT create duplicate sales or duplicate tickets.

### 13.4 Invalid Late Confirmation

A confirmation presented after the transaction has fully expired and its inventory has been authoritatively released MUST NOT be silently accepted as though the original reservation still existed.

---

## 14. Release Policy

### 14.1 Unconfirmed Transactions

A valid unconfirmed hold or checkout transaction MAY be released explicitly where its state permits release.

### 14.2 Idempotency

Release MUST be idempotent.

Repeated release of the same logical transaction MUST NOT create additional side effects.

### 14.3 Sold Inventory

`release()` MUST NOT be used to return a confirmed sale to availability.

Confirmed-ticket cancellation and inventory resale are separate operations.

---

## 15. Pricing Policy

### 15.1 Event-Controlled Pricing

Event owners MAY assign price categories to sections or individual seats.

### 15.2 Price Snapshot

A successful hold MUST snapshot the authoritative event-controlled price applicable to that inventory for the transaction.

A later event price change MUST NOT silently reprice an already valid hold, protected checkout, or reconciliation transaction.

### 15.3 Future Inventory

Price changes apply to future acquisitions unless an explicit repricing workflow is invoked with customer consent.

### 15.4 Partner Fees

Partner-controlled service fees remain outside TktSync's event-inventory price authority.

Dynamic pricing is outside the defined MVP scope.

---

## 16. Allocation and Block Policy

### 16.1 Purpose

Inventory MAY be blocked or allocated for uses including:

- sponsor;
- VIP;
- media;
- complimentary;
- fighter/team;
- production.

### 16.2 Normal Blocking

Normal blocking MAY transition only inventory that is currently eligible for blocking.

An administrator MUST NOT silently displace:

- an active customer hold;
- protected checkout inventory;
- a reconciliation transaction;
- a confirmed sale.

### 16.3 Concurrent Block and Hold

If a buyer acquisition and administrative block contend for the same inventory, exactly one authoritative state transition may succeed.

Administrative status does not automatically grant priority over an already successful customer acquisition.

### 16.4 Bulk Block Operations

A bulk block request SHOULD be all-or-nothing by default.

If one or more requested inventory units conflict with active obligations, the platform SHOULD reject the requested bulk transition and identify the conflicting units rather than silently applying an unexpected partial block.

Any future partial-application mode MUST be explicit.

### 16.5 Allocation Issuance

Blocked inventory MUST NOT enter normal public partner sales.

Where an allocation represents a comp, sponsor, VIP, media, or similar entitlement, an explicit non-public issuance process MAY convert allocated inventory into a ticket without making that inventory publicly available first.

Such issuance MUST preserve auditability and inventory uniqueness.

---

## 17. Administrative Inventory Policy

### 17.1 Stable Identity After Sale Opens

Once an event is on sale, inventory identities become stable.

The platform MUST prevent configuration changes that would invalidate existing holds, tickets, or historical meaning.

Examples of prohibited silent changes include:

- deleting a sold seat;
- renaming a sold seat into a different physical identity;
- remapping a confirmed ticket to another seat without an explicit reissue/reassignment process;
- reducing GA capacity below existing obligations.

### 17.2 Venue Template Changes

Changes to a reusable venue template MUST NOT automatically mutate an already-live event inventory.

Event-specific changes MUST be explicit and validated against existing obligations.

### 17.3 GA Capacity Changes

GA capacity MAY be increased.

GA capacity MUST NOT be reduced below:

`sold + held + checkout-protected + reconciling + blocked/allocated`

### 17.4 Privileged Override

Any privileged override that changes otherwise protected state MUST:

- require elevated authorization;
- require an explicit reason;
- preserve previous-state history;
- create an immutable audit event;
- never erase the original transaction history.

---

## 18. Partner Neutrality Policy

All ticketing partners operate against the same authoritative event inventory unless the event owner has explicitly created channel-specific allocations.

TktSync MUST NOT provide hidden inventory priority to one partner over another.

Where two valid requests contend for the same unit, the authoritative atomic acquisition determines the winner.

Channel-specific inventory behavior MUST be represented as explicit event configuration rather than hidden platform favoritism.

---

## 19. Partner Suspension and Credential Policy

### 19.1 Authentication

Partner inventory-changing operations MUST be authenticated and scoped to the authorized partner and event context.

### 19.2 Operational Disable

An operationally disabled partner MUST be prevented from initiating new inventory acquisitions.

Existing legitimate holds and checkout transactions SHOULD be allowed to resolve under a documented shutdown policy rather than being arbitrarily destroyed.

### 19.3 Security Revocation

A credential-compromise or security-revocation action MAY terminate broader partner capability immediately.

Security revocation is distinct from ordinary operational disable and MUST be auditable.

### 19.4 Cross-Partner Isolation

A partner MUST NOT read or mutate another partner's private transaction details except where a deliberately shared event-level availability view contains no partner-private transaction information.

---

## 20. Idempotency Policy

All externally retriable state-changing operations MUST support idempotent semantics.

This includes, at minimum:

- hold creation;
- hold modification;
- begin checkout;
- release;
- confirmation;
- cancellation/void where implemented;
- ticket credential reissue;
- scan/admission operations where client retry is possible.

### 20.1 Same Request, Same Logical Result

Retrying the same logical request with the same idempotency identity MUST return the same logical outcome.

### 20.2 Key Reuse With Different Intent

Reusing an idempotency identity with materially different request content MUST be rejected as a conflict rather than interpreted as a new transaction.

### 20.3 Network Ambiguity

A timeout or lost response does not establish that an operation failed.

Clients MUST be able to retry or query authoritative transaction status without creating duplicate business effects.

---

## 21. Time Policy

### 21.1 Server Authority

TktSync server time is authoritative for:

- hold expiry;
- checkout protection;
- payment retry;
- reconciliation;
- sale windows;
- scan timestamps;
- audit timestamps.

### 21.2 Client Timers

Client countdowns are display aids and MUST NOT be the authoritative expiry mechanism.

### 21.3 Accepted-Before-Deadline Rule

When TktSync authoritatively accepts a valid transition before its deadline, subsequent internal processing time MUST NOT retroactively invalidate that accepted operation.

---

## 22. Sale Cancellation, Void, and Resale Policy

A confirmed sale is not an ordinary hold and MUST NOT be released through hold-release semantics.

### 22.1 Ticket Cancellation / Void

A confirmed ticket MAY be transitioned to an explicit `VOIDED` or `CANCELLED` entitlement state when authorized.

### 22.2 Financial Refund

Refund processing belongs to the ticketing partner and is separate from TktSync ticket-state changes.

### 22.3 Inventory Re-release

Voiding a ticket and making its inventory available for resale are separate business decisions.

A voided reserved seat MUST NOT automatically return to public availability unless an explicit resale policy or operation authorizes the transition.

This separation preserves accurate financial, inventory, and audit history.

---

## 23. Ticket Identity and QR Credential Policy

### 23.1 Ticket Identity

A confirmed sale creates a stable ticket entitlement identity.

### 23.2 Credential Separation

A QR code is an admission credential representing a ticket; it is not the ticket's historical identity itself.

### 23.3 Credential Reissue

Where a QR credential is compromised, lost, or intentionally rotated:

- the ticket identity MAY remain unchanged;
- the previous credential MUST become invalid;
- a new credential MAY be issued;
- the reissue MUST be auditable.

### 23.4 Credential Content

QR credentials SHOULD avoid unnecessary customer PII and SHOULD be designed so that possession of decoded display data does not permit unauthorized modification of ticket state.

---

## 24. Admission and Scan Policy

### 24.1 Authoritative Admission

For a single-entry ticket, exactly one distinct authoritative admission may succeed unless the event explicitly enables another admission policy.

### 24.2 Concurrent Scans

If the same valid ticket is presented concurrently at multiple gates, exactly one distinct admission may succeed.

Subsequent distinct attempts MUST return an already-admitted/duplicate result.

### 24.3 Scan Retry Idempotency

A technical retry of the same scan operation MUST return the same logical result as the original request.

A scanner MUST NOT receive a misleading "duplicate fraud" result merely because the first successful scan response was lost and the same request was retried.

### 24.4 Ticket Validation

Admission MUST account for, as applicable:

- event identity;
- ticket identity;
- credential validity;
- ticket void/cancel state;
- event cancel state;
- prior authoritative admission;
- authorization of the scanning context.

### 24.5 Connectivity

The MVP authoritative duplicate-prevention guarantee requires connectivity to TktSync at validation time.

Offline scanning MUST NOT be represented as providing the same cross-gate duplicate-prevention guarantee unless a separate offline-consistency design explicitly provides it.

### 24.6 Manual Override

Any manual or supervisor admission override MUST:

- require elevated authorization;
- require or record a reason;
- preserve the original validation result;
- create an audit event;
- avoid rewriting prior scan history.

---

## 25. White-Label Seat Selection Policy

For partners without their own seat-selection interface, TktSync MAY provide a white-label mobile-first selection experience.

The white-label flow MUST preserve responsibility boundaries:

1. buyer enters from the partner;
2. buyer views event availability;
3. buyer selects inventory;
4. TktSync acquires a valid hold;
5. buyer returns to the partner with transaction identity;
6. partner performs checkout and payment;
7. partner confirms or releases through TktSync.

The white-label selector MUST NOT silently become the payment authority.

Partner branding and checkout ownership remain with the partner.

---

## 26. Security and Transaction-Scope Policy

### 26.1 Least Authority

Every actor MUST receive only the authority required for its platform role.

### 26.2 Client Exposure

Administrative and partner-secret credentials MUST NOT be exposed to untrusted buyer-facing clients.

### 26.3 Transaction Scope

Buyer-facing continuation tokens SHOULD be narrowly scoped to the applicable event, partner, transaction, and permitted operation.

### 26.4 Credential Revocation

Partner, scanner, and administrative credentials MUST support revocation without rewriting historical actions performed while those credentials were valid.

---

## 27. Customer Data Policy

Because ticketing partners own the customer relationship, TktSync SHOULD minimize customer PII.

Inventory operations SHOULD rely on partner-controlled references such as:

- partner customer reference;
- partner order reference;
- buyer/session reference;

unless additional customer data is necessary for ticket issuance, accreditation, compliance, or event operation.

TktSync MUST NOT require unrelated customer PII merely to create inventory ownership.

QR credentials SHOULD NOT expose unnecessary PII.

---

## 28. Failure and Degradation Policy

### 28.1 Inventory Authority Unavailable

If TktSync cannot safely determine authoritative inventory state, it MUST NOT create new sales or holds based solely on stale cache or client state.

Temporary inability to sell is preferable to overselling.

### 28.2 Confirmation Uncertainty

If a partner cannot determine whether confirmation succeeded, it MUST retry idempotently or query authoritative status rather than issue a second logical sale.

### 28.3 Expiry Worker Delay

Failure or delay of background expiry processing MUST NOT by itself permit already-reserved inventory to be sold twice.

Logical expiry and authoritative transition rules remain decisive.

### 28.4 Realtime Failure

Failure of realtime delivery MUST degrade freshness, not correctness.

### 28.5 Scanner Authority Unavailable

Where authoritative online validation is unavailable, ordinary admission cannot claim full duplicate-prevention guarantees. Any exceptional manual admission follows the explicit override policy.

---

## 29. Audit Policy

### 29.1 Append-Only History

Meaningful state-changing activity MUST produce durable audit evidence.

Audit history MUST NOT be rewritten merely to make current state appear clean.

### 29.2 Minimum Audit Context

Audit events SHOULD record, where applicable:

- actor;
- actor type;
- partner or event context;
- operation;
- inventory or ticket identity;
- previous state;
- new state;
- server timestamp;
- hold/order/sale correlation identifiers;
- idempotency identity where relevant;
- reason for privileged or exceptional actions.

### 29.3 Exceptional Actions

Manual overrides, security revocations, ticket reissues, forced cancellations, and privileged inventory changes MUST be explicitly identifiable in audit history.

---

## 30. Reporting and Export Policy

### 30.1 Single Semantic Vocabulary

The following concepts MUST have consistent meaning across APIs, dashboards, audit logs, exports, and partner reporting:

- available;
- held;
- committing;
- reconciling;
- blocked/allocated;
- sold;
- voided/cancelled;
- admitted/scanned.

### 30.2 Sold Means Confirmed

Inventory MUST NOT be reported as sold merely because a buyer has entered checkout or because an external payment attempt may have succeeded.

`SOLD` means TktSync has authoritatively confirmed the sale.

### 30.3 Exports Are Derived Views

Accreditation and reporting exports are snapshots derived from authoritative records.

An export MUST NOT become an alternate source of truth and MUST NOT mutate inventory or ticket state.

Exports SHOULD include generation timestamp and sufficient event context to establish the snapshot they represent.

---

## 31. Process: Venue and Event Configuration

The normal configuration process is:

1. create or select venue;
2. define sections and zones;
3. define reserved seats, tables, and/or GA capacity;
4. configure orientation;
5. configure pricing categories;
6. create event-specific inventory;
7. configure allocations and blocks;
8. validate event inventory consistency;
9. place event `ON_SALE`.

Opening sales MUST be rejected while required inventory configuration is invalid.

---

## 32. Process: Reserved-Seat Purchase

1. partner or white-label UI reads availability;
2. buyer selects one or more seats;
3. partner requests an atomic hold;
4. TktSync either acquires the complete requested set or rejects the hold;
5. TktSync returns hold identity and authoritative expiry;
6. buyer decides whether to purchase;
7. before payment, partner requests checkout protection;
8. TktSync transitions the transaction to `COMMITTING`;
9. partner initiates payment;
10. on success, partner confirms;
11. TktSync transitions inventory to `SOLD`;
12. TktSync creates ticket identity and active QR credential;
13. on explicit failure/release, applicable inventory returns to availability;
14. on uncertain payment outcome, the transaction follows reconciliation policy.

---

## 33. Process: GA Purchase

1. partner reads available GA quantity;
2. buyer selects quantity;
3. partner requests an atomic hold for the full quantity;
4. TktSync validates that sufficient quantity remains;
5. if sufficient, the entire quantity becomes held;
6. if insufficient, zero quantity is silently acquired and the partner is informed of the conflict;
7. checkout protection, confirmation, release, retry, and reconciliation follow the same transaction policies as reserved seating.

---

## 34. Process: Mixed Purchase

Where one customer transaction contains GA and reserved inventory:

1. all requested components are evaluated as one logical acquisition;
2. the complete requested set MUST succeed or the acquisition MUST fail without silently creating a partial order;
3. checkout protection MUST cover the full logical transaction;
4. confirmation MUST create exactly one logical sale outcome for the transaction while issuing the appropriate ticket identities for its components.

---

## 35. Process: Hold Expiry

For an ordinary hold:

`HELD -> EXPIRED -> AVAILABLE`

This transition applies only when the hold has not already entered protected checkout.

Expiration MUST be determined by authoritative platform time.

A delayed cleanup worker MUST NOT make an expired hold valid indefinitely; equally, no other sale may assume ownership until the authoritative state transition is safely established.

---

## 36. Process: Protected Checkout and Reconciliation

The authoritative customer-protective flow is:

`AVAILABLE -> HELD -> COMMITTING -> SOLD`

Failure paths include:

`HELD -> EXPIRED -> AVAILABLE`

`COMMITTING -> PAYMENT_RETRY -> COMMITTING`

`COMMITTING -> RECONCILING -> SOLD`

`COMMITTING -> RECONCILING -> AVAILABLE`

The purpose of `COMMITTING` is to protect a customer who has already committed to payment.

The purpose of `RECONCILING` is to prevent immediate resale while the payment outcome is uncertain.

Neither state may be used to hold inventory indefinitely.

---

## 37. Process: Allocation and Non-Public Issuance

1. event administrator selects available inventory;
2. TktSync atomically blocks or allocates the requested inventory;
3. blocked inventory disappears from normal public acquisition;
4. where permitted, an authorized allocation may later be converted through an explicit non-public issuance process;
5. issuance creates stable ticket identity and audit history without first publishing the inventory as generally available.

---

## 38. Process: Admission Validation

1. scanner captures QR credential;
2. scanner submits credential to TktSync;
3. TktSync validates event, ticket, credential, status, and admission history;
4. if valid and unused, TktSync atomically records admission and returns success;
5. if already admitted by a distinct prior operation, TktSync returns duplicate/already-admitted;
6. if the request is a retry of the same successful scan operation, TktSync returns the original logical success;
7. invalid, revoked, voided, cancelled, or wrong-event credentials are rejected with business-meaningful status;
8. all meaningful scan outcomes are auditable.

---

## 39. Process: Ticket Void and Reissue

### 39.1 Void
1. authorized actor requests void/cancellation;
2. TktSync validates authority and current ticket state;
3. ticket entitlement becomes void/cancelled;
4. active QR credential becomes invalid;
5. audit history records the action;
6. any decision to return the physical inventory to sale is handled separately.

### 39.2 Credential Reissue
1. authorized actor requests credential rotation;
2. stable ticket identity remains;
3. existing QR credential is revoked;
4. replacement credential is issued;
5. both revocation and issuance are audited.

---

## 40. Business-Meaningful Error Policy

External consumers MUST be able to distinguish materially different business outcomes.

The platform SHOULD expose stable machine-readable outcomes equivalent to:

- `INVENTORY_UNAVAILABLE`
- `INSUFFICIENT_GA_QUANTITY`
- `HOLD_EXPIRED`
- `HOLD_NOT_OWNED`
- `CHECKOUT_WINDOW_EXPIRED`
- `RECONCILIATION_EXPIRED`
- `ALREADY_CONFIRMED`
- `EVENT_NOT_ON_SALE`
- `EVENT_PAUSED`
- `EVENT_CANCELLED`
- `PARTNER_DISABLED`
- `IDEMPOTENCY_CONFLICT`
- `TICKET_INVALID`
- `TICKET_VOID`
- `TICKET_ALREADY_ADMITTED`
- `CREDENTIAL_REVOKED`
- `AUTHORITY_TEMPORARILY_UNAVAILABLE`

HTTP status alone MUST NOT be the only semantic signal where consumers need different recovery behavior.

---

## 41. Non-Negotiable Platform Invariants

The following invariants govern all implementations.

1. TktSync is the authoritative inventory and validation system.
2. Availability does not reserve inventory.
3. Only an authoritative acquisition operation creates temporary ownership.
4. Every acquisition preserves inventory uniqueness.
5. Multi-item acquisition is all-or-nothing unless the customer explicitly requests a different transaction.
6. Inventory is never silently substituted.
7. A reserved seat has exactly one authoritative event-specific state at a time.
8. GA inventory never falls below zero.
9. Holds are temporary, scoped, and non-transferable except through explicit policy.
10. Hold modification cannot create unlimited reservation duration.
11. Protected checkout is granted before a partner initiates chargeable payment.
12. Original hold expiry does not dispossess a customer already in valid protected checkout.
13. Payment retry is bounded.
14. Payment uncertainty receives bounded reconciliation.
15. Reconciliation cannot reserve inventory indefinitely.
16. After authoritative reconciliation expiry, late confirmation cannot silently reclaim released inventory.
17. All externally retriable mutations are idempotent.
18. Network timeout does not prove business failure.
19. Confirmation creates exactly one logical sale.
20. Event-controlled price is snapshotted for acquired inventory.
21. Normal public sales cannot acquire blocked inventory.
22. Administrative actions do not silently displace active buyer obligations.
23. Venue and inventory identities remain historically stable after sales activity begins.
24. GA capacity cannot be reduced below existing obligations.
25. Realtime delivery improves freshness but never determines ownership.
26. Partners operate neutrally against the shared pool unless explicit allocations exist.
27. Partners cannot mutate another partner's private transactions.
28. Customer PII is minimized.
29. Ticket identity and QR credential are separate concepts.
30. Revoked QR credentials do not erase ticket history.
31. A single-entry ticket permits one distinct authoritative admission.
32. Concurrent distinct duplicate scans have one successful admission.
33. Technical scan retries are idempotent.
34. Full MVP duplicate-prevention guarantees require authoritative connectivity.
35. Sold-ticket voiding and inventory resale are separate actions.
36. Event cancellation never erases sale or audit history.
37. Reports and exports are derived views, not alternate authorities.
38. Exceptional administrative actions are explicit, reasoned, privileged, and audited.
39. When state is unsafe or ambiguous, the platform favors temporary unavailability over overselling or silent loss of legitimate buyer rights.
40. Every actor—partner, white-label interface, administrator, scanner, realtime client, background expiry process, and recovery process—must preserve these invariants.

---

## 42. Configuration Parameters, Not Policy Gaps

The following values are intentionally configurable and do not represent unresolved platform semantics:

- ordinary hold duration;
- checkout protection duration;
- payment retry duration;
- reconciliation grace duration;
- maximum hold quantity;
- maximum active holds;
- maximum total reservation age;
- rate-limit thresholds;
- event sale start/end times;
- whether an event permits ticket re-entry;
- whether voided inventory may be returned to resale;
- event-specific allocation categories.

Configured values MUST remain within platform safety limits and MUST NOT defeat the bounded-duration policies in this document.

---

## 43. MVP Scope Alignment

The governing policies support the technical brief's MVP scope:

- one event and one venue;
- visual floor-plan builder;
- GA and/or reserved seating;
- two to three partner integrations;
- realtime locking with hold timers;
- white-label buyer seat selection;
- VIP/sponsor/comp allocation blocking;
- QR generation and scan validation;
- admin dashboard;
- audit log;
- accreditation export.

The following remain outside MVP unless explicitly added:

- native mobile application;
- self-serve partner onboarding;
- payment processing;
- dynamic pricing;
- enterprise analytics;
- multilingual support;
- CRM/messaging;
- ticket transfer marketplace or secondary resale.

Out-of-scope capabilities MUST NOT be simulated by weakening the authoritative inventory, validation, audit, or responsibility-boundary policies.

---

## 44. Policy Precedence

Where product behavior is not explicitly described in the MVP brief, platform behavior MUST be resolved in accordance with the following precedence:

1. prevent overselling and contradictory inventory ownership;
2. preserve legitimate buyer rights already accepted by TktSync;
3. avoid charging or confirming against inventory that is not protected;
4. preserve authoritative ticket and admission integrity;
5. maintain partner neutrality;
6. preserve immutable auditability;
7. minimize customer harm during infrastructure uncertainty;
8. fail safely rather than infer irreversible state from stale or incomplete information.

Any future policy amendment that changes these semantics must be versioned and reviewed as a platform-contract change.

---

**End of Document**

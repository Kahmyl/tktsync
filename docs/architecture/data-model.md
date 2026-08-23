# Relational Data Model

> Governing PostgreSQL schema, relational constraints, locking targets, migration order, and persistence invariants for the TktSync MVP.

Normative parents, in precedence order:

1. [Platform Policy](platform-policy.md)
2. [Logical Domain Model](domain-model.md)
3. [System Architecture and Transactional Design](system-design.md)

Product basis: TktSync Technical Brief (2026).

---

## 1. Purpose

This document defines the canonical PostgreSQL relational data model for TktSync.

It specifies:

- table boundaries;
- column semantics and data types;
- primary keys;
- foreign keys;
- deletion behavior;
- check constraints;
- unique and partial unique indexes;
- operational indexes;
- cross-table constraint triggers;
- immutable-field rules;
- authoritative and derived data boundaries;
- migration ordering;
- relational enforcement of the approved platform policies, logical-domain invariants, and transactional architecture.

This document is intended to prevent implementation drift. Database migrations, ORM models, query code, API persistence logic, background workers, reporting queries, and administrative tooling MUST conform to this schema unless this specification is formally revised.

The schema does not redefine product policy or domain semantics. Where this document conflicts with an upstream governing document, the upstream document takes precedence and this schema must be amended.

---

## 2. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

- **MUST / MUST NOT** define required relational behavior.
- **SHOULD / SHOULD NOT** define expected design unless a reviewed exception exists.
- **MAY** defines permitted behavior.

---

## 3. Database Baseline

The authoritative database is PostgreSQL, managed through Supabase for the MVP.

The schema assumes support for:

- `uuid`;
- `jsonb`;
- `timestamptz`;
- partial indexes;
- expression indexes;
- generated identity columns;
- row-level locks;
- deferrable constraint triggers;
- `num_nonnulls(...)`;
- `clock_timestamp()`;
- `gen_random_uuid()` or equivalent UUID generation.

All authoritative business writes occur against the primary PostgreSQL authority.

Read replicas, caches, realtime channels, browser state, exports, and analytics stores are not authoritative.

---

## 4. General Schema Conventions

### 4.1 Primary Keys

Business tables SHALL use:

~~~sql
id uuid PRIMARY KEY DEFAULT gen_random_uuid()
~~~

unless a table is naturally one-to-one with its parent, in which case the parent foreign key MAY also be the primary key.

UUID identity MUST NOT be derived from mutable labels.

### 4.2 Timestamps

All authoritative timestamps SHALL use:

~~~sql
timestamptz
~~~

Timestamps represent absolute instants.

Application display timezones are separate presentation concerns.

### 4.3 Authoritative Time

Defaults such as `now()` MAY populate ordinary creation timestamps.

Deadline acceptance logic MUST still use the System Architecture rule: true current database time is evaluated after required locks are acquired, using `clock_timestamp()` or equivalent.

### 4.4 Money

Monetary values SHALL use integer minor units:

~~~sql
amount_minor bigint
currency char(3)
~~~

Floating-point database types MUST NOT be used for authoritative money.

### 4.5 State Columns

The MVP SHALL use `text` columns with named `CHECK` constraints for canonical states rather than PostgreSQL enum types.

This preserves explicit relational validation while allowing controlled future state additions through migrations without enum-specific operational constraints.

### 4.6 JSONB

`jsonb` is permitted for:

- floor-plan geometry/presentation snapshots;
- commercial metadata snapshots;
- audit metadata;
- outbox payloads;
- optional accreditation metadata;
- non-authoritative extension metadata.

`jsonb` MUST NOT replace relational columns for state, ownership, inventory quantity, ticket identity, or other invariants that PostgreSQL can enforce relationally.

### 4.7 Mutable vs Historical Rows

Business history is normally preserved.

Normal application code MUST NOT hard-delete:

- Reservations with business history;
- Sales;
- SaleItems;
- NonPublicIssuances;
- TicketEntitlements;
- QRCredentials;
- ScanAttempts;
- Admissions;
- AuditEvents;
- historical ReservedInventoryClaims.

Draft-only structural objects may be physically removed where the governing domain permits hard deletion.

---

## Part I: SCHEMA MAP

## 5. Canonical Table Groups

~~~text
VENUE & LAYOUT
├── venues
├── venue_layout_versions
├── venue_layout_sections
├── venue_layout_rows
├── venue_layout_tables
├── venue_layout_seats
└── venue_layout_ga_zones

EVENT & INVENTORY
├── events
├── event_transaction_policies
├── event_layout_snapshots
├── event_sections
├── event_price_tiers
├── reserved_inventory_units
├── ga_inventory_pools
└── ga_shared_inventory

IDENTITY, PARTNERS & AUTHORIZATION
├── app_users
├── platform_user_roles
├── event_staff_assignments
├── partners
├── partner_credentials
├── partner_event_access
└── buyer_selection_sessions

PARTNER WEBHOOKS
├── partner_webhook_endpoints
├── partner_webhook_signing_secrets
├── partner_webhook_subscriptions
├── webhook_deliveries
└── webhook_delivery_attempts

RESTRICTIONS & ALLOCATIONS
├── inventory_restrictions
├── blocks
├── allocations
├── block_reserved_units
├── allocation_reserved_units
├── ga_block_buckets
└── ga_allocation_buckets

RESERVATION & CHECKOUT
├── reservations
├── reservation_items
└── checkout_attempts

COMMERCIAL & ENTITLEMENTS
├── sales
├── sale_items
├── non_public_issuances
├── non_public_issuance_items
├── ticket_entitlements
├── ticket_attendee_details
└── qr_credentials

RESERVED-INVENTORY CLAIM HISTORY
└── reserved_inventory_claims

ADMISSION
├── scan_attempts
└── admissions

INFRASTRUCTURE & HISTORY
├── idempotency_operations
├── audit_events
└── outbox_events
~~~

---

## 6. Why Reserved Inventory Uses a Claim Table

`reserved_inventory_units` SHALL represent stable event-specific seat identity.

It SHALL NOT contain five mutable nullable columns such as:

- `current_reservation_id`;
- `current_block_id`;
- `current_allocation_id`;
- `current_sale_id`;
- `current_issuance_id`.

That approach creates circular foreign keys, makes historical resale difficult to explain, and introduces many invalid nullable combinations.

Instead, `reserved_inventory_claims` stores claim history.

A reserved inventory unit has:

- **no active claim** => `AVAILABLE`;
- active `RESERVATION` claim => `RESERVED`;
- active `BLOCK` claim => `BLOCKED`;
- active `ALLOCATION` claim => `ALLOCATED`;
- active `SALE` claim => `SOLD`;
- active `ISSUANCE` claim => `ISSUED`.

A partial unique index permits at most one active claim for a unit.

The stable `reserved_inventory_units` row remains the transaction lock target.

This model satisfies the logical requirement that a reserved unit has exactly one current disposition and at most one current consuming claim without duplicating current state across multiple tables.

---

## 7. Why GA Uses Source Buckets

General Admission (GA) is quantity-based and MUST NOT create synthetic pre-sale seat rows.

The schema represents a GA pool with:

- one `ga_inventory_pools` aggregate/lock row;
- one `ga_shared_inventory` row;
- zero or more block buckets;
- zero or more allocation buckets.

The pool capacity is distributed across these current buckets.

A deferred database constraint validates that the sum of current quantities equals pool capacity at transaction commit.

This avoids maintaining the same "allocated" or "reserved" number in multiple unrelated authoritative counters.

---

## Part II: VENUE & LAYOUT

## 8. `venues`

Reusable physical venue identity.

| Column | Type | Null | Meaning |
|---|---|---:|---|
| `id` | `uuid` | No | Stable venue identity |
| `name` | `text` | No | Display name |
| `address_text` | `text` | Yes | Optional descriptive address |
| `metadata` | `jsonb` | No | Non-authoritative venue metadata |
| `created_at` | `timestamptz` | No | Creation time |
| `updated_at` | `timestamptz` | No | Last allowed metadata update |

Defaults:

~~~sql
metadata DEFAULT '{}'::jsonb
created_at DEFAULT now()
updated_at DEFAULT now()
~~~

Deletion:

- `RESTRICT` once referenced by a layout/event history;
- draft-only unused venue deletion MAY be allowed by application guard.

Indexes:

~~~sql
INDEX venues_name_idx (name)
~~~

---

## 9. `venue_layout_versions`

Versioned floor-plan baseline.

| Column | Type | Null | Meaning |
|---|---|---:|---|
| `id` | `uuid` | No | Layout version identity |
| `venue_id` | `uuid` | No | Parent Venue |
| `version_number` | `integer` | No | Venue-local version |
| `state` | `text` | No | `DRAFT`, `PUBLISHED`, `RETIRED` |
| `geometry_json` | `jsonb` | No | Third-party/visual layout representation |
| `content_hash` | `bytea` | Yes | Optional immutable content digest |
| `created_at` | `timestamptz` | No | Created |
| `published_at` | `timestamptz` | Yes | Publication time |
| `retired_at` | `timestamptz` | Yes | Retirement time |

Constraints:

~~~sql
CHECK (state IN ('DRAFT','PUBLISHED','RETIRED'))
CHECK (version_number > 0)
UNIQUE (venue_id, version_number)
UNIQUE (id, venue_id)
~~~

Foreign key:

~~~sql
venue_id REFERENCES venues(id) ON DELETE RESTRICT
~~~

Immutability:

- when `state = 'PUBLISHED'` or `RETIRED`, material physical identity and `geometry_json` MUST NOT be modified in place;
- a trigger SHALL reject material updates to published layout versions;
- material changes create a new version.

---

## 10. `venue_layout_sections`

Reusable section/zone definitions within one layout version.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `layout_version_id` | `uuid` | No |
| `object_key` | `text` | No |
| `name` | `text` | No |
| `section_kind` | `text` | No |
| `sort_order` | `integer` | No |
| `metadata` | `jsonb` | No |

`section_kind` values:

- `RESERVED`;
- `GA`;
- `TABLE`;
- `MIXED_VISUAL`.

The value is primarily layout classification. Event inventory remains authoritative.

Constraints:

~~~sql
UNIQUE (layout_version_id, object_key)
UNIQUE (id, layout_version_id)
CHECK (section_kind IN ('RESERVED','GA','TABLE','MIXED_VISUAL'))
~~~

FK:

~~~sql
layout_version_id REFERENCES venue_layout_versions(id) ON DELETE CASCADE
~~~

Cascade is safe only because published-layout deletion itself is prohibited by higher-level trigger/guard.

---

## 11. `venue_layout_rows`

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `layout_version_id` | `uuid` | No |
| `section_id` | `uuid` | No |
| `object_key` | `text` | No |
| `label` | `text` | No |
| `sort_order` | `integer` | No |
| `metadata` | `jsonb` | No |

Constraints:

~~~sql
UNIQUE (layout_version_id, object_key)
UNIQUE (id, layout_version_id)
~~~

Same-layout integrity MUST be enforced through composite foreign keys or a scope-validation trigger.

---

## 12. `venue_layout_tables`

Visual/physical table groupings.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `layout_version_id` | `uuid` | No |
| `section_id` | `uuid` | No |
| `object_key` | `text` | No |
| `label` | `text` | No |
| `metadata` | `jsonb` | No |

Constraints:

~~~sql
UNIQUE (layout_version_id, object_key)
UNIQUE (id, layout_version_id)
~~~

A table does not itself imply whole-table sale in the MVP.

---

## 13. `venue_layout_seats`

Reusable physical seat definitions.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `layout_version_id` | `uuid` | No |
| `section_id` | `uuid` | No |
| `row_id` | `uuid` | Yes |
| `table_id` | `uuid` | Yes |
| `object_key` | `text` | No |
| `seat_label` | `text` | No |
| `sort_order` | `integer` | No |
| `metadata` | `jsonb` | No |

Constraints:

~~~sql
UNIQUE (layout_version_id, object_key)
UNIQUE (id, layout_version_id)
~~~

`row_id` and `table_id` are optional because different third-party floor-plan models may represent seat groupings differently.

All referenced layout components MUST belong to the same layout version.

---

## 14. `venue_layout_ga_zones`

Reusable GA/standing zone definitions.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `layout_version_id` | `uuid` | No |
| `section_id` | `uuid` | No |
| `object_key` | `text` | No |
| `name` | `text` | No |
| `default_capacity` | `integer` | Yes |
| `metadata` | `jsonb` | No |

Constraints:

~~~sql
UNIQUE (layout_version_id, object_key)
CHECK (default_capacity IS NULL OR default_capacity >= 0)
~~~

The default capacity is only a venue-layout hint. Event GA capacity is event-specific.

---

## Part III: USERS, PARTNERS & AUTHORIZATION

## 15. `app_users`

Application-level identity mapping for human operators.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `auth_provider` | `text` | No |
| `auth_subject` | `text` | No |
| `display_name` | `text` | Yes |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (auth_provider, auth_subject)
CHECK (state IN ('ACTIVE','DISABLED'))
~~~

This keeps domain identity independent from a particular authentication provider while remaining compatible with Supabase Auth.

---

## 16. `platform_user_roles`

Platform-global human authority.

| Column | Type | Null |
|---|---|---:|
| `user_id` | `uuid` | No |
| `role` | `text` | No |
| `created_at` | `timestamptz` | No |

Primary key:

~~~sql
PRIMARY KEY (user_id, role)
~~~

Current role:

~~~sql
CHECK (role IN ('PLATFORM_ADMIN'))
~~~

FK:

~~~sql
user_id REFERENCES app_users(id) ON DELETE RESTRICT
~~~

---

## 17. `event_staff_assignments`

Event-scoped human roles.

| Column | Type | Null |
|---|---|---:|
| `event_id` | `uuid` | No |
| `user_id` | `uuid` | No |
| `role` | `text` | No |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `disabled_at` | `timestamptz` | Yes |

Primary key:

~~~sql
PRIMARY KEY (event_id, user_id, role)
~~~

Roles:

- `EVENT_MANAGER`;
- `BOX_OFFICE`;
- `GATE_SUPERVISOR`;
- `SCANNER`;
- `VIEWER`.

Constraint:

~~~sql
CHECK (state IN ('ACTIVE','DISABLED'))
~~~

Deletion:

- historical assignment rows SHOULD be disabled rather than deleted once used in audit/admission history.

---

## 18. `partners`

Ticketing partner identity.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `name` | `text` | No |
| `state` | `text` | No |
| `metadata` | `jsonb` | No |
| `created_at` | `timestamptz` | No |
| `disabled_at` | `timestamptz` | Yes |

Constraint:

~~~sql
CHECK (state IN ('ACTIVE','DISABLED'))
~~~

Partner rows are never replaced by API keys.

---

## 19. `partner_credentials`

Revocable machine credentials.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `key_id` | `text` | No |
| `secret_hash` | `bytea` | No |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `last_used_at` | `timestamptz` | Yes |
| `revoked_at` | `timestamptz` | Yes |

Constraints:

~~~sql
UNIQUE (key_id)
CHECK (state IN ('ACTIVE','REVOKED'))
~~~

FK:

~~~sql
partner_id REFERENCES partners(id) ON DELETE RESTRICT
~~~

Raw secrets MUST NOT be persisted.

Credential revocation MUST NOT cascade to Reservations.

---

## 20. `partner_event_access`

Partner permission to transact on one Event.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `disabled_at` | `timestamptz` | Yes |

Constraints:

~~~sql
UNIQUE (partner_id, event_id)
UNIQUE (id, partner_id, event_id)
CHECK (state IN ('ACTIVE','DISABLED'))
~~~

Operational disable does not delete existing transaction history.

---

## 21. `buyer_selection_sessions`

White-label buyer capability sessions.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `token_hash` | `bytea` | No |
| `token_key_version` | `integer` | No |
| `buyer_session_ref` | `text` | Yes |
| `state` | `text` | No |
| `expires_at` | `timestamptz` | No |
| `created_at` | `timestamptz` | No |
| `revoked_at` | `timestamptz` | Yes |

Constraints:

~~~sql
UNIQUE (token_hash)
UNIQUE (id, partner_id, event_id)
CHECK (token_key_version > 0)
CHECK (state IN ('ACTIVE','REVOKED','EXPIRED'))
~~~

Buyer capabilities do not contain Partner secret credentials.

`token_key_version` identifies the deterministic HMAC key version defined by the Security & Authentication Specification. Once the capability is issued, `expires_at` MUST NOT be extended in place because it participates in the signed capability; a longer session requires a new BuyerSelectionSession. Revocation may shorten effective lifetime.

---

## Part IV: EVENT & INVENTORY

## 22. `events`

Authoritative event lifecycle row and lifecycle lock target.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `venue_id` | `uuid` | No |
| `name` | `text` | No |
| `state` | `text` | No |
| `starts_at` | `timestamptz` | Yes |
| `ends_at` | `timestamptz` | Yes |
| `sales_open_at` | `timestamptz` | Yes |
| `sales_close_at` | `timestamptz` | Yes |
| `admission_open_at` | `timestamptz` | Yes |
| `admission_close_at` | `timestamptz` | Yes |
| `timezone_name` | `text` | Yes |
| `admission_policy` | `text` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |
| `cancelled_at` | `timestamptz` | Yes |
| `completed_at` | `timestamptz` | Yes |

States:

- `DRAFT`;
- `ON_SALE`;
- `PAUSED`;
- `SALES_CLOSED`;
- `COMPLETED`;
- `CANCELLED`.

Constraints:

~~~sql
CHECK (state IN (
  'DRAFT','ON_SALE','PAUSED',
  'SALES_CLOSED','COMPLETED','CANCELLED'
))
CHECK (admission_policy IN ('SINGLE_ENTRY'))
CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
CHECK (sales_close_at IS NULL OR sales_open_at IS NULL OR sales_close_at >= sales_open_at)
CHECK (admission_close_at IS NULL OR admission_open_at IS NULL OR admission_close_at >= admission_open_at)
~~~

The Event row is the shared/exclusive lifecycle gate defined by the System Architecture specification.

Currency is intentionally not fixed at Event row level because the upstream specifications do not require a single-currency Event. Transaction currency coherence is enforced on each Reservation and its items.

---

## 23. `event_transaction_policies`

Persisted event-level safety configuration.

| Column | Type | Null |
|---|---|---:|
| `event_id` | `uuid` | No |
| `hold_duration_seconds` | `integer` | No |
| `checkout_protection_seconds` | `integer` | No |
| `payment_retry_seconds` | `integer` | No |
| `reconciliation_seconds` | `integer` | No |
| `max_reservation_lifetime_seconds` | `integer` | No |
| `max_hold_quantity` | `integer` | No |
| `max_active_reservations_per_partner` | `integer` | No |
| `max_active_reservations_per_buyer_session` | `integer` | No |
| `allow_voided_inventory_rerelease` | `boolean` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Primary key:

~~~sql
event_id PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE
~~~

Constraints:

~~~sql
CHECK (hold_duration_seconds > 0)
CHECK (checkout_protection_seconds > 0)
CHECK (payment_retry_seconds >= 0)
CHECK (reconciliation_seconds > 0)
CHECK (max_reservation_lifetime_seconds >= hold_duration_seconds)
CHECK (max_hold_quantity > 0)
CHECK (max_active_reservations_per_partner > 0)
CHECK (max_active_reservations_per_buyer_session > 0)
~~~

Rate-limit thresholds may remain operational configuration rather than authoritative event business state.

---

## 24. `event_layout_snapshots`

One event-specific frozen floor-plan snapshot.

| Column | Type | Null |
|---|---|---:|
| `event_id` | `uuid` | No |
| `source_layout_version_id` | `uuid` | No |
| `snapshot_json` | `jsonb` | No |
| `content_hash` | `bytea` | Yes |
| `finalized_at` | `timestamptz` | Yes |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Primary key:

~~~sql
event_id PRIMARY KEY
~~~

FKs:

~~~sql
event_id REFERENCES events(id) ON DELETE CASCADE
source_layout_version_id REFERENCES venue_layout_versions(id) ON DELETE RESTRICT
~~~

While Event is safely `DRAFT` with no protected history, this row MAY be regenerated.

Once Event has protected history or opens for sale:

- `source_layout_version_id`;
- `snapshot_json`;
- physical identity content

become immutable.

A trigger SHALL enforce snapshot freeze.

---

## 25. `event_sections`

Event-specific section identities derived from the snapshot.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `source_layout_section_id` | `uuid` | Yes |
| `snapshot_object_key` | `text` | No |
| `name` | `text` | No |
| `default_price_tier_id` | `uuid` | Yes |
| `sort_order` | `integer` | No |
| `metadata` | `jsonb` | No |

Constraints:

~~~sql
UNIQUE (event_id, snapshot_object_key)
UNIQUE (id, event_id)
~~~

The stable ID is authoritative. Names remain presentation metadata subject to live-event identity rules.

---

## 26. `event_price_tiers`

Event-controlled pricing categories.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `code` | `text` | No |
| `name` | `text` | No |
| `amount_minor` | `bigint` | No |
| `currency` | `char(3)` | No | Transaction currency for this price tier |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (event_id, code)
UNIQUE (id, event_id)
CHECK (amount_minor >= 0)
CHECK (currency ~ '^[A-Z]{3}$')
CHECK (state IN ('ACTIVE','RETIRED'))
~~~

Retiring a tier prevents future assignment but does not invalidate historical ReservationItem snapshots.

### 26.1 Effective Pricing Precedence

Commercial hold pricing SHALL resolve in this order:

Reserved shared inventory:

~~~text
ReservedInventoryUnit.price_tier_override
    > EventSection.default_price_tier
~~~

Reserved channel Allocation:

~~~text
ReservedInventoryUnit.price_tier_override
    > EventSection.default_price_tier
~~~

Allocation changes eligibility/source, not Event-controlled commercial pricing.

GA shared inventory:

~~~text
GAInventoryPool.price_tier
~~~

GA channel Allocation:

~~~text
GAInventoryPool.price_tier
~~~

Allocation changes eligibility/source, not Event-controlled commercial pricing.

A commercial hold MUST fail configuration validation if no effective active price tier exists for an item that requires commercial pricing.

The resolved amount/currency/label is snapshotted into ReservationItem and is not recalculated later.

---

## 27. `reserved_inventory_units`

Stable event-specific reserved-seat identity.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `event_section_id` | `uuid` | No |
| `source_venue_seat_id` | `uuid` | Yes |
| `snapshot_object_key` | `text` | No |
| `row_label` | `text` | Yes |
| `seat_label` | `text` | No |
| `table_label` | `text` | Yes |
| `display_label` | `text` | No |
| `price_tier_override_id` | `uuid` | Yes |
| `metadata` | `jsonb` | No |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (event_id, snapshot_object_key)
UNIQUE (id, event_id)
~~~

This table does **not** contain current `disposition`.

Current disposition is derived from the one active row in `reserved_inventory_claims`.

No active claim means `AVAILABLE`.

Physical identity columns become immutable once the Event has protected business history.

---

## 28. `ga_inventory_pools`

GA aggregate root and canonical row-lock target.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `event_section_id` | `uuid` | No |
| `source_ga_zone_id` | `uuid` | Yes |
| `snapshot_object_key` | `text` | No |
| `name` | `text` | No |
| `capacity` | `integer` | No |
| `price_tier_id` | `uuid` | Yes | Event-controlled GA price tier |
| `metadata` | `jsonb` | No |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (event_id, snapshot_object_key)
UNIQUE (id, event_id)
CHECK (capacity >= 0)
~~~

All GA quantity mutations MUST lock this row before modifying source buckets.

---

## 29. `ga_shared_inventory`

The shared public source bucket for one GA pool.

| Column | Type | Null |
|---|---|---:|
| `ga_pool_id` | `uuid` | No |
| `available_quantity` | `integer` | No |
| `active_reserved_quantity` | `integer` | No |
| `sold_current_quantity` | `integer` | No |
| `updated_at` | `timestamptz` | No |

Primary key:

~~~sql
ga_pool_id PRIMARY KEY REFERENCES ga_inventory_pools(id) ON DELETE CASCADE
~~~

Constraints:

~~~sql
CHECK (available_quantity >= 0)
CHECK (active_reserved_quantity >= 0)
CHECK (sold_current_quantity >= 0)
~~~

Non-public issuance is not created directly from the shared bucket; it originates from an eligible non-public Allocation.

---

## Part V: RESTRICTIONS & ALLOCATIONS

## 30. `inventory_restrictions`

Common restriction identity.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `kind` | `text` | No |
| `state` | `text` | No |
| `purpose` | `text` | No |
| `reason` | `text` | Yes |
| `created_by_user_id` | `uuid` | No |
| `created_at` | `timestamptz` | No |
| `released_at` | `timestamptz` | Yes |
| `closed_at` | `timestamptz` | Yes |

Constraints:

~~~sql
CHECK (kind IN ('BLOCK','ALLOCATION'))
CHECK (state IN ('ACTIVE','RELEASED','CLOSED'))
UNIQUE (id, event_id)
~~~

Purpose is event-configurable but typical values include:

- `SPONSOR`;
- `VIP`;
- `MEDIA`;
- `COMP`;
- `TEAM`;
- `PRODUCTION`;
- `HOUSE`;
- `SECURITY`;
- `CHANNEL`;
- `OTHER`.

Purpose is not used as an authorization substitute; `kind` and Allocation mode are authoritative.

---

## 31. `blocks`

Block subtype.

| Column | Type | Null |
|---|---|---:|
| `restriction_id` | `uuid` | No |

Primary key and FK:

~~~sql
restriction_id PRIMARY KEY
REFERENCES inventory_restrictions(id) ON DELETE RESTRICT
~~~

A constraint trigger SHALL ensure parent `kind = 'BLOCK'`.

---

## 32. `allocations`

Allocation subtype.

| Column | Type | Null |
|---|---|---:|
| `restriction_id` | `uuid` | No |
| `mode` | `text` | No |
| `partner_id` | `uuid` | Yes |
| `release_destination_kind` | `text` | No |
| `release_destination_allocation_id` | `uuid` | Yes |

Primary key:

~~~sql
restriction_id PRIMARY KEY
~~~

Modes:

- `CHANNEL`;
- `NON_PUBLIC`.

Constraints:

~~~sql
CHECK (mode IN ('CHANNEL','NON_PUBLIC'))
CHECK (release_destination_kind IN ('SHARED','ALLOCATION'))

CHECK (
  (mode = 'CHANNEL' AND partner_id IS NOT NULL)
  OR
  (mode = 'NON_PUBLIC' AND partner_id IS NULL)
)

CHECK (
  (release_destination_kind = 'SHARED' AND release_destination_allocation_id IS NULL)
  OR
  (release_destination_kind = 'ALLOCATION' AND release_destination_allocation_id IS NOT NULL)
)

CHECK (release_destination_allocation_id IS NULL
       OR release_destination_allocation_id <> restriction_id)
~~~

A trigger SHALL validate:

- parent restriction kind is `ALLOCATION`;
- destination Allocation belongs to the same Event;
- release-destination chains do not form a cycle;
- channel Allocation Partner has applicable Event access when created;

---

## 33. `block_reserved_units`

Historical reserved-seat membership in a Block.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `block_id` | `uuid` | No |
| `reserved_inventory_unit_id` | `uuid` | No |
| `assigned_at` | `timestamptz` | No |
| `released_at` | `timestamptz` | Yes |

Constraints:

~~~sql
UNIQUE (block_id, reserved_inventory_unit_id)
UNIQUE (id, reserved_inventory_unit_id)
~~~

Current block ownership is represented by an active `BLOCK` ReservedInventoryClaim referencing this row.

---

## 34. `allocation_reserved_units`

Historical reserved-seat membership in an Allocation.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `allocation_id` | `uuid` | No |
| `reserved_inventory_unit_id` | `uuid` | No |
| `assigned_at` | `timestamptz` | No |
| `released_at` | `timestamptz` | Yes |

Constraints:

~~~sql
UNIQUE (allocation_id, reserved_inventory_unit_id)
UNIQUE (id, reserved_inventory_unit_id)
~~~

Membership persists even when the seat later becomes reserved or sold.

This historical membership is required to restore allocation-sourced Reservations correctly and to explain channel allocation history.

---

## 35. `ga_block_buckets`

Quantity assigned from a GA pool to one Block.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `block_id` | `uuid` | No |
| `ga_pool_id` | `uuid` | No |
| `assigned_quantity` | `integer` | No |
| `blocked_quantity` | `integer` | No |
| `released_quantity` | `integer` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (block_id, ga_pool_id)
UNIQUE (id, ga_pool_id)

CHECK (assigned_quantity >= 0)
CHECK (blocked_quantity >= 0)
CHECK (released_quantity >= 0)
CHECK (assigned_quantity = blocked_quantity + released_quantity)
~~~

`released_quantity` is historical allocation of quantity that no longer consumes the current pool through this Block.

---

## 36. `ga_allocation_buckets`

Quantity assigned from a GA pool to one Allocation.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `allocation_id` | `uuid` | No |
| `ga_pool_id` | `uuid` | No |
| `assigned_quantity` | `integer` | No |
| `available_quantity` | `integer` | No |
| `active_reserved_quantity` | `integer` | No |
| `sold_current_quantity` | `integer` | No |
| `issued_current_quantity` | `integer` | No |
| `released_quantity` | `integer` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (allocation_id, ga_pool_id)
UNIQUE (id, ga_pool_id)

CHECK (assigned_quantity >= 0)
CHECK (available_quantity >= 0)
CHECK (active_reserved_quantity >= 0)
CHECK (sold_current_quantity >= 0)
CHECK (issued_current_quantity >= 0)
CHECK (released_quantity >= 0)

CHECK (
  assigned_quantity =
    available_quantity
  + active_reserved_quantity
  + sold_current_quantity
  + issued_current_quantity
  + released_quantity
)
~~~

Interpretation:

- `available_quantity`: still available within this Allocation;
- `active_reserved_quantity`: held/committing/reconciling from this Allocation;
- `sold_current_quantity`: currently consuming capacity through confirmed commercial sale;
- `issued_current_quantity`: currently consuming capacity through non-public issuance;
- `released_quantity`: no longer consumes current capacity through this Allocation.

When an Allocation is released while a Reservation remains active:

- unreserved `available_quantity` moves to release destination and increments `released_quantity`;
- `active_reserved_quantity` remains until the buyer resolves;
- a later Reservation release moves the quantity to the recorded destination and increments `released_quantity`;
- confirmation moves `active_reserved_quantity -> sold_current_quantity`.

---

## 37. Deferred GA Pool Balance Constraint

A deferrable constraint trigger SHALL validate at transaction commit:

~~~text
ga_inventory_pools.capacity
=
ga_shared_inventory.available_quantity
+ ga_shared_inventory.active_reserved_quantity
+ ga_shared_inventory.sold_current_quantity
+ SUM(ga_block_buckets.blocked_quantity)
+ SUM(ga_allocation_buckets.available_quantity)
+ SUM(ga_allocation_buckets.active_reserved_quantity)
+ SUM(ga_allocation_buckets.sold_current_quantity)
+ SUM(ga_allocation_buckets.issued_current_quantity)
~~~

`released_quantity` is historical and is intentionally excluded.

The trigger SHALL be:

- `DEFERRABLE`;
- `INITIALLY DEFERRED`.

This permits one transaction to move quantity between buckets while requiring the final committed state to balance exactly.

Every GA quantity mutation MUST still lock `ga_inventory_pools` first, as required by the architecture.

### 37.1 Deferred GA Active-Reservation Reconciliation

A second deferrable constraint trigger SHALL verify that GA `active_reserved_quantity` matches persisted active GA ReservationItems.

For the shared bucket:

~~~text
ga_shared_inventory.active_reserved_quantity
=
SUM(active ReservationItem.quantity
    WHERE inventory_kind = GA
      AND source_kind = SHARED
      AND ga_pool_id = pool)
~~~

For each Allocation bucket:

~~~text
ga_allocation_buckets.active_reserved_quantity
=
SUM(active ReservationItem.quantity
    WHERE inventory_kind = GA
      AND source_ga_allocation_bucket_id = bucket)
~~~

"Active" for this physical accounting trigger means the Reservation row is persisted in:

- `HELD`;
- `COMMITTING`;
- `PAYMENT_RETRY`;
- `RECONCILING`;

and the ReservationItem has `removed_at IS NULL`.

This trigger validates materialized database accounting, not effective deadline rights. An overdue but not-yet-materialized Reservation remains physically reserved until the canonical expiry transaction restores its source; command guards still enforce logical expiry independently.

---

## 38. GA Capacity Change Semantics

Increasing capacity:

- locks GA pool;
- increments `capacity`;
- increments shared `available_quantity` by the same amount.

Reducing capacity:

- locks GA pool;
- MAY reduce only by quantity that can be safely removed from current uncommitted availability;
- ordinary reduction consumes shared `available_quantity`;
- MUST NOT reduce current sold, issued, reserved, blocked, or allocated obligations;
- deferred pool-balance validation MUST pass.

This directly enforces the Platform Policy rule that GA capacity cannot be reduced below existing obligations.

---

## Part VI: RESERVATION & CHECKOUT

## 39. `reservations`

Authoritative buyer acquisition transaction.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `buyer_selection_session_id` | `uuid` | Yes |
| `partner_customer_ref` | `text` | Yes |
| `partner_order_ref` | `text` | Yes |
| `buyer_session_ref` | `text` | Yes |
| `continuation_token_hash` | `bytea` | No |
| `continuation_token_key_version` | `integer` | No |
| `currency` | `char(3)` | No |
| `state` | `text` | No |
| `hold_expires_at` | `timestamptz` | No |
| `payment_retry_expires_at` | `timestamptz` | Yes |
| `reconciliation_expires_at` | `timestamptz` | Yes |
| `max_lifetime_at` | `timestamptz` | No |
| `terminal_reason` | `text` | Yes |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |
| `confirmed_at` | `timestamptz` | Yes |
| `released_at` | `timestamptz` | Yes |
| `expired_at` | `timestamptz` | Yes |

States:

- `HELD`;
- `COMMITTING`;
- `PAYMENT_RETRY`;
- `RECONCILING`;
- `CONFIRMED`;
- `RELEASED`;
- `EXPIRED`.

Constraints:

~~~sql
CHECK (state IN (
  'HELD','COMMITTING','PAYMENT_RETRY','RECONCILING',
  'CONFIRMED','RELEASED','EXPIRED'
))
CHECK (currency ~ '^[A-Z]{3}$')

CHECK (hold_expires_at <= max_lifetime_at)
CHECK (payment_retry_expires_at IS NULL OR payment_retry_expires_at <= max_lifetime_at)
CHECK (reconciliation_expires_at IS NULL OR reconciliation_expires_at <= max_lifetime_at)

CHECK (
  (state = 'CONFIRMED') = (confirmed_at IS NOT NULL)
)

CHECK (
  (state = 'RELEASED') = (released_at IS NOT NULL)
)

CHECK (
  (state = 'EXPIRED') = (expired_at IS NOT NULL)
)

CHECK (
  state NOT IN ('RELEASED','EXPIRED')
  OR terminal_reason IS NOT NULL
)

UNIQUE (id, event_id)
UNIQUE (id, event_id, partner_id)
UNIQUE (continuation_token_hash)
CHECK (continuation_token_key_version > 0)
~~~

`continuation_token_hash` backs the opaque hold token returned by TktSync. `continuation_token_key_version` identifies the server HMAC key version required to deterministically recover/verify the token after a lost response. The raw token is returned to the caller but is not stored. The token identifies the Reservation for continuation; possession of it alone does not bypass Partner or BuyerSelectionSession authorization.

Partner-provided customer/order references are opaque correlation values at this schema layer. Their uniqueness semantics SHALL be defined by the subsequent Partner API contract rather than silently assumed by the database.

Buyer session FK SHALL verify matching Partner/Event scope using composite keys.

No `payment_status` column is authoritative here. Payment remains external.

---

## 40. Reservation Deadline Indexes

Worker candidate discovery SHALL use partial indexes:

~~~sql
CREATE INDEX reservations_hold_due_idx
ON reservations(hold_expires_at, id)
WHERE state = 'HELD';

CREATE INDEX reservations_retry_due_idx
ON reservations(payment_retry_expires_at, id)
WHERE state = 'PAYMENT_RETRY';

CREATE INDEX reservations_reconcile_due_idx
ON reservations(reconciliation_expires_at, id)
WHERE state = 'RECONCILING';
~~~

A similar index MAY support `COMMITTING` timeout through the active CheckoutAttempt deadline.

These indexes improve cleanup discovery but do not define effective expiry.

---

## 41. `reservation_items`

Immutable acquisition selection/snapshot rows, except for removal marker while Reservation remains modifiable.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `reservation_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `inventory_kind` | `text` | No |
| `reserved_inventory_unit_id` | `uuid` | Yes |
| `ga_pool_id` | `uuid` | Yes |
| `quantity` | `integer` | No |
| `source_kind` | `text` | No |
| `source_allocation_reserved_unit_id` | `uuid` | Yes |
| `source_ga_allocation_bucket_id` | `uuid` | Yes |
| `price_tier_id` | `uuid` | Yes |
| `unit_amount_minor` | `bigint` | No |
| `currency` | `char(3)` | No | Snapshotted transaction currency |
| `price_tier_label_snapshot` | `text` | Yes |
| `commercial_terms` | `jsonb` | No |
| `created_at` | `timestamptz` | No |
| `removed_at` | `timestamptz` | Yes |

Inventory kinds:

- `RESERVED`;
- `GA`.

Source kinds:

- `SHARED`;
- `ALLOCATION`.

Constraints:

~~~sql
CHECK (inventory_kind IN ('RESERVED','GA'))
CHECK (source_kind IN ('SHARED','ALLOCATION'))
CHECK (quantity > 0)
CHECK (unit_amount_minor >= 0)
CHECK (currency ~ '^[A-Z]{3}$')
~~~

Reserved item exclusive arc:

~~~sql
CHECK (
  (inventory_kind = 'RESERVED'
   AND reserved_inventory_unit_id IS NOT NULL
   AND ga_pool_id IS NULL
   AND quantity = 1)
  OR
  (inventory_kind = 'GA'
   AND reserved_inventory_unit_id IS NULL
   AND ga_pool_id IS NOT NULL)
)
~~~

Source exclusive arc:

~~~sql
CHECK (
  (source_kind = 'SHARED'
   AND source_allocation_reserved_unit_id IS NULL
   AND source_ga_allocation_bucket_id IS NULL)
  OR
  (source_kind = 'ALLOCATION'
   AND (
     (inventory_kind = 'RESERVED'
      AND source_allocation_reserved_unit_id IS NOT NULL
      AND source_ga_allocation_bucket_id IS NULL)
     OR
     (inventory_kind = 'GA'
      AND source_allocation_reserved_unit_id IS NULL
      AND source_ga_allocation_bucket_id IS NOT NULL)
   ))
)
~~~

Event/currency consistency:

- ReservationItem Event MUST equal Reservation Event;
- inventory source MUST belong to the same Event;
- `currency` MUST equal Reservation currency;
- every active item in one Reservation therefore shares one transaction currency;
- price tier, when present, MUST belong to the same Event and its currency MUST match the Reservation currency.

These MUST be enforced with composite foreign keys where practical and a constraint trigger for any remaining cross-table checks.

Modification history:

- removed items are marked with `removed_at`;
- item rows MUST NOT be repurposed to a different physical seat;
- newly added inventory creates a new ReservationItem;
- existing item price snapshot is never rewritten.

Useful indexes:

~~~sql
INDEX reservation_items_active_by_reservation
(reservation_id, id)
WHERE removed_at IS NULL

INDEX reservation_items_reserved_unit_idx
(reserved_inventory_unit_id)
WHERE reserved_inventory_unit_id IS NOT NULL

INDEX reservation_items_ga_pool_idx
(ga_pool_id)
WHERE ga_pool_id IS NOT NULL
~~~

---

## 42. `checkout_attempts`

One bounded protected-payment attempt.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `reservation_id` | `uuid` | No |
| `attempt_number` | `integer` | No |
| `state` | `text` | No |
| `protection_expires_at` | `timestamptz` | No |
| `partner_payment_ref` | `text` | Yes |
| `partner_outcome_code` | `text` | Yes |
| `started_at` | `timestamptz` | No |
| `completed_at` | `timestamptz` | Yes |
| `created_at` | `timestamptz` | No |

States:

- `ACTIVE`;
- `PAYMENT_FAILED`;
- `UNCERTAIN`;
- `CONFIRMED`;
- `ABANDONED`.

Constraints:

~~~sql
UNIQUE (reservation_id, attempt_number)
CHECK (attempt_number > 0)
CHECK (state IN (
  'ACTIVE','PAYMENT_FAILED','UNCERTAIN','CONFIRMED','ABANDONED'
))
~~~

Critical partial unique index:

~~~sql
CREATE UNIQUE INDEX checkout_attempts_one_active_uq
ON checkout_attempts(reservation_id)
WHERE state = 'ACTIVE';
~~~

Worker index:

~~~sql
CREATE INDEX checkout_attempts_active_due_idx
ON checkout_attempts(protection_expires_at, reservation_id)
WHERE state = 'ACTIVE';
~~~

No card authorization, settlement, processor fee, or refund state is modeled.

---

## Part VII: SALES, ISSUANCE & TICKETS

## 43. `sales`

Immutable commercial confirmation fact.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `reservation_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `partner_order_ref` | `text` | Yes |
| `partner_payment_ref` | `text` | Yes |
| `currency` | `char(3)` | No | Confirmed Reservation transaction currency |
| `confirmed_at` | `timestamptz` | No |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (reservation_id)
UNIQUE (id, event_id)
CHECK (currency ~ '^[A-Z]{3}$')
~~~

Composite FK SHALL ensure Sale Reservation, Event, and Partner are the same Reservation scope.

Partner-provided order/payment references are persisted for reconciliation and reporting but are not assigned universal database uniqueness semantics in this specification. Different Partner protocols may define those identifiers differently. The subsequent Partner API contract MAY add a Partner-specific uniqueness rule where the integration contract guarantees it.

Idempotency and `UNIQUE (reservation_id)` remain the universal exactly-one Sale safeguards.

Sale rows are immutable in ordinary operation.

---

## 44. `sale_items`

Immutable confirmed commercial inventory line.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `sale_id` | `uuid` | No |
| `reservation_item_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `inventory_kind` | `text` | No |
| `reserved_inventory_unit_id` | `uuid` | Yes |
| `ga_pool_id` | `uuid` | Yes |
| `quantity` | `integer` | No |
| `source_allocation_id` | `uuid` | Yes |
| `unit_amount_minor` | `bigint` | No |
| `currency` | `char(3)` | No | Confirmed transaction currency |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (reservation_item_id)
UNIQUE (id, event_id)

CHECK (inventory_kind IN ('RESERVED','GA'))
CHECK (quantity > 0)
CHECK (unit_amount_minor >= 0)

CHECK (
  (inventory_kind = 'RESERVED'
   AND reserved_inventory_unit_id IS NOT NULL
   AND ga_pool_id IS NULL
   AND quantity = 1)
  OR
  (inventory_kind = 'GA'
   AND reserved_inventory_unit_id IS NULL
   AND ga_pool_id IS NOT NULL)
)
~~~

A deferred integrity trigger SHALL verify that a SaleItem exactly represents the referenced active ReservationItem snapshot at confirmation time.

SaleItem is immutable afterward.

---

## 45. `non_public_issuances`

Immutable non-commercial entitlement issuance.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `allocation_id` | `uuid` | No |
| `issued_by_user_id` | `uuid` | No |
| `recipient_ref` | `text` | Yes |
| `reason` | `text` | Yes |
| `issued_at` | `timestamptz` | No |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
UNIQUE (id, event_id)
~~~

A trigger SHALL verify:

- Allocation mode = `NON_PUBLIC`;
- Allocation/Event scopes match;
- Event is eligible for issuance.

No Sale row is created.

---

## 46. `non_public_issuance_items`

Inventory line produced through non-public issuance.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `issuance_id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `inventory_kind` | `text` | No |
| `reserved_inventory_unit_id` | `uuid` | Yes |
| `ga_pool_id` | `uuid` | Yes |
| `allocation_reserved_unit_id` | `uuid` | Yes |
| `ga_allocation_bucket_id` | `uuid` | Yes |
| `quantity` | `integer` | No |
| `created_at` | `timestamptz` | No |

Constraints:

~~~sql
CHECK (inventory_kind IN ('RESERVED','GA'))
CHECK (quantity > 0)

CHECK (
  (inventory_kind = 'RESERVED'
   AND reserved_inventory_unit_id IS NOT NULL
   AND allocation_reserved_unit_id IS NOT NULL
   AND ga_pool_id IS NULL
   AND ga_allocation_bucket_id IS NULL
   AND quantity = 1)
  OR
  (inventory_kind = 'GA'
   AND reserved_inventory_unit_id IS NULL
   AND allocation_reserved_unit_id IS NULL
   AND ga_pool_id IS NOT NULL
   AND ga_allocation_bucket_id IS NOT NULL)
)
~~~

Each item MUST originate from the issuance Allocation.

---

## 47. `ticket_entitlements`

Stable ticket identity.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `origin_sale_item_id` | `uuid` | Yes |
| `origin_issuance_item_id` | `uuid` | Yes |
| `replaces_ticket_entitlement_id` | `uuid` | Yes |
| `inventory_kind` | `text` | No |
| `reserved_inventory_unit_id` | `uuid` | Yes |
| `ga_pool_id` | `uuid` | Yes |
| `status` | `text` | No |
| `created_at` | `timestamptz` | No |
| `voided_at` | `timestamptz` | Yes |
| `void_reason` | `text` | Yes |

Constraints:

~~~sql
CHECK (num_nonnulls(origin_sale_item_id, origin_issuance_item_id) = 1)
CHECK (inventory_kind IN ('RESERVED','GA'))
CHECK (status IN ('ACTIVE','VOIDED'))

CHECK (
  (inventory_kind = 'RESERVED'
   AND reserved_inventory_unit_id IS NOT NULL
   AND ga_pool_id IS NULL)
  OR
  (inventory_kind = 'GA'
   AND reserved_inventory_unit_id IS NULL
   AND ga_pool_id IS NOT NULL)
)

CHECK (
  (status = 'VOIDED') = (voided_at IS NOT NULL)
)
~~~

A constraint trigger SHALL verify that:

- the Ticket Event/inventory matches its origin SaleItem or NonPublicIssuanceItem;
- any replacement ticket references the same Event and physical inventory;
- replacement ancestry does not form a cycle;
- the replaced ticket is already `VOIDED` before the replacement becomes `ACTIVE`.

Replacement and current-entitlement constraints:

~~~sql
CREATE UNIQUE INDEX ticket_one_replacement_child_uq
ON ticket_entitlements(replaces_ticket_entitlement_id)
WHERE replaces_ticket_entitlement_id IS NOT NULL;

CREATE UNIQUE INDEX ticket_one_active_reserved_unit_uq
ON ticket_entitlements(reserved_inventory_unit_id)
WHERE inventory_kind = 'RESERVED'
  AND status = 'ACTIVE';
~~~

Replacement rules:

- `replaces_ticket_entitlement_id`, when present, MUST reference a `VOIDED` TicketEntitlement for the same Event, same physical inventory, and same immutable Sale/Issuance origin lineage;
- replacement creates a new Ticket identity; it never rewrites the voided identity;
- a replacement chain MUST NOT form a cycle.

The one-active-reserved-unit index is stronger than permanently limiting a SaleItem to one historical Ticket row. It allows a voided ticket to be replaced while ensuring that one reserved physical unit never has two active entitlements.

GA quantity `N` creates `N` independently admissible TicketEntitlement rows referencing the same GA SaleItem/IssuanceItem. Historical replacement may create additional rows only through an explicit authorized replacement workflow.

Ticket rows remain after void.

---

## 48. `ticket_attendee_details`

Optional PII/accreditation extension isolated from inventory identity.

| Column | Type | Null |
|---|---|---:|
| `ticket_entitlement_id` | `uuid` | No |
| `partner_attendee_ref` | `text` | Yes |
| `display_name` | `text` | Yes |
| `accreditation_data` | `jsonb` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |

Primary key:

~~~sql
ticket_entitlement_id PRIMARY KEY
~~~

This table MAY be unused where no attendee/accreditation data is required.

Customer PII MUST NOT be moved into core inventory tables merely for convenience.

---

## 49. `qr_credentials`

Revocable credential history.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `ticket_entitlement_id` | `uuid` | No |
| `token_hash` | `bytea` | No |
| `token_key_version` | `integer` | No |
| `status` | `text` | No |
| `issued_at` | `timestamptz` | No |
| `superseded_at` | `timestamptz` | Yes |
| `revoked_at` | `timestamptz` | Yes |
| `created_at` | `timestamptz` | No |

States:

- `ACTIVE`;
- `SUPERSEDED`;
- `REVOKED`.

Constraints:

~~~sql
UNIQUE (token_hash)
CHECK (token_key_version > 0)
CHECK (status IN ('ACTIVE','SUPERSEDED','REVOKED'))
~~~

Critical partial unique index:

~~~sql
CREATE UNIQUE INDEX qr_credentials_one_active_uq
ON qr_credentials(ticket_entitlement_id)
WHERE status = 'ACTIVE';
~~~

Status/timestamp checks SHALL ensure:

- `SUPERSEDED` => `superseded_at IS NOT NULL`;
- `REVOKED` => `revoked_at IS NOT NULL`;
- `ACTIVE` => both terminal timestamps null.

Raw QR secret tokens MUST NOT be stored where a secure digest lookup is practical. `token_key_version` identifies the deterministic HMAC key version used to regenerate/verify the active QR representation after a lost response.

---

## Part VIII: RESERVED INVENTORY CLAIMS

## 50. `reserved_inventory_claims`

Append-preserved claim history and current reserved-seat disposition.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `reserved_inventory_unit_id` | `uuid` | No |
| `claim_type` | `text` | No |
| `reservation_item_id` | `uuid` | Yes |
| `block_reserved_unit_id` | `uuid` | Yes |
| `allocation_reserved_unit_id` | `uuid` | Yes |
| `sale_item_id` | `uuid` | Yes |
| `issuance_item_id` | `uuid` | Yes |
| `activated_at` | `timestamptz` | No |
| `ended_at` | `timestamptz` | Yes |
| `end_reason` | `text` | Yes |

Claim types:

- `RESERVATION`;
- `BLOCK`;
- `ALLOCATION`;
- `SALE`;
- `ISSUANCE`.

Exclusive source constraint:

~~~sql
CHECK (
  num_nonnulls(
    reservation_item_id,
    block_reserved_unit_id,
    allocation_reserved_unit_id,
    sale_item_id,
    issuance_item_id
  ) = 1
)
~~~

Type/source mapping:

~~~sql
CHECK (
  (claim_type = 'RESERVATION'
   AND reservation_item_id IS NOT NULL)
  OR
  (claim_type = 'BLOCK'
   AND block_reserved_unit_id IS NOT NULL)
  OR
  (claim_type = 'ALLOCATION'
   AND allocation_reserved_unit_id IS NOT NULL)
  OR
  (claim_type = 'SALE'
   AND sale_item_id IS NOT NULL)
  OR
  (claim_type = 'ISSUANCE'
   AND issuance_item_id IS NOT NULL)
)
~~~

Critical invariant:

~~~sql
CREATE UNIQUE INDEX reserved_inventory_one_active_claim_uq
ON reserved_inventory_claims(reserved_inventory_unit_id)
WHERE ended_at IS NULL;
~~~

This database constraint is the final safeguard against two simultaneous current claims on one named seat.

No row is created for `AVAILABLE`.

---

## 51. Reserved Disposition View

A canonical read-only view SHOULD expose current state:

~~~text
no active claim          -> AVAILABLE
RESERVATION active claim -> RESERVED
BLOCK active claim       -> BLOCKED
ALLOCATION active claim  -> ALLOCATED
SALE active claim        -> SOLD
ISSUANCE active claim    -> ISSUED
~~~

Representative view name:

~~~sql
v_reserved_inventory_current_state
~~~

The view is derived.

The active claim rows and stable inventory identity are authoritative.

---

## 52. Reserved Claim Integrity Trigger

A deferrable constraint trigger SHALL validate active claim/source integrity at commit.

At minimum:

### `RESERVATION`

- source ReservationItem is active (`removed_at IS NULL`);
- ReservationItem inventory kind is `RESERVED`;
- ReservationItem references this exact ReservedInventoryUnit;
- Reservation effective persisted state is one of:
  - `HELD`;
  - `COMMITTING`;
  - `PAYMENT_RETRY`;
  - `RECONCILING`.

### `BLOCK`

- referenced Block membership references this unit;
- Block parent restriction is `ACTIVE`;
- membership is not released.

### `ALLOCATION`

- referenced Allocation membership references this unit;
- Allocation is currently eligible to hold unreserved allocation inventory;
- membership is not released.

### `SALE`

- SaleItem is reserved inventory;
- SaleItem references this exact unit;
- the corresponding Sale exists.

Ticket void does **not** invalidate a `SALE` claim automatically.

### `ISSUANCE`

- NonPublicIssuanceItem references this exact unit;
- issuance exists.

Ticket void does **not** invalidate an `ISSUANCE` claim automatically.

Claim transitions occur in the same transaction as their source business transition.

---

## Part IX: IDEMPOTENCY

## 53. `idempotency_operations`

Durable external-operation identity and replay result.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `scope_kind` | `text` | No |
| `partner_id` | `uuid` | Yes |
| `app_user_id` | `uuid` | Yes |
| `buyer_selection_session_id` | `uuid` | Yes |
| `operation_type` | `text` | No |
| `idempotency_key` | `text` | No |
| `request_hash` | `bytea` | No |
| `execution_state` | `text` | No |
| `result_code` | `text` | Yes |
| `result_entity_type` | `text` | Yes |
| `result_entity_id` | `uuid` | Yes |
| `result_payload` | `jsonb` | Yes |
| `created_at` | `timestamptz` | No |
| `completed_at` | `timestamptz` | Yes |

Scope kinds:

- `PARTNER`;
- `USER`;
- `BUYER_SESSION`.

Execution states:

- `IN_PROGRESS`;
- `SUCCEEDED`;
- `FAILED_BUSINESS`.

Constraint:

~~~sql
CHECK (
  num_nonnulls(partner_id, app_user_id, buyer_selection_session_id) = 1
)

CHECK (
  (scope_kind = 'PARTNER' AND partner_id IS NOT NULL)
  OR
  (scope_kind = 'USER' AND app_user_id IS NOT NULL)
  OR
  (scope_kind = 'BUYER_SESSION' AND buyer_selection_session_id IS NOT NULL)
)

CHECK (execution_state IN (
  'IN_PROGRESS','SUCCEEDED','FAILED_BUSINESS'
))
~~~

Unique operation identity:

~~~sql
CREATE UNIQUE INDEX idempotency_scope_operation_uq
ON idempotency_operations (
  scope_kind,
  COALESCE(partner_id, app_user_id, buyer_selection_session_id),
  operation_type,
  idempotency_key
);
~~~

### 53.1 Stable Business Failures

A deterministic business rejection MAY commit as `FAILED_BUSINESS`, allowing the same idempotency key to replay the same business outcome.

Examples:

- inventory unavailable;
- event not on sale;
- hold expired;
- idempotency request conflict.

Transient infrastructure errors, deadlocks that are still within retry handling, and unavailable database authority MUST NOT be converted into a permanent business result merely for convenience.

### 53.2 Completion Atomicity

For a successful business mutation:

- business rows;
- audit facts;
- outbox facts;
- idempotency `SUCCEEDED` result

commit in the same PostgreSQL transaction.

---

## Part X: ADMISSION

## 54. `scan_attempts`

One authoritative validation request/result.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `scanner_user_id` | `uuid` | No |
| `ticket_entitlement_id` | `uuid` | Yes |
| `qr_credential_id` | `uuid` | Yes |
| `idempotency_operation_id` | `uuid` | No |
| `result` | `text` | No |
| `gate_reference` | `text` | Yes |
| `metadata` | `jsonb` | No |
| `occurred_at` | `timestamptz` | No |

Representative result values:

- `ADMITTED`;
- `ALREADY_ADMITTED`;
- `INVALID_CREDENTIAL`;
- `CREDENTIAL_REVOKED`;
- `CREDENTIAL_SUPERSEDED`;
- `TICKET_VOID`;
- `WRONG_EVENT`;
- `EVENT_CANCELLED`;
- `ADMISSION_NOT_OPEN`;
- `NOT_AUTHORIZED`;
- `MANUAL_OVERRIDE_ADMITTED`.

Constraints:

~~~sql
UNIQUE (idempotency_operation_id)
~~~

A failed or duplicate ScanAttempt does not create an Admission.

Raw presented QR secrets MUST NOT be retained in general scan metadata.

---

## 55. `admissions`

Authoritative admission fact.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | No |
| `ticket_entitlement_id` | `uuid` | No |
| `scan_attempt_id` | `uuid` | No |
| `status` | `text` | No |
| `admitted_at` | `timestamptz` | No |
| `reversed_at` | `timestamptz` | Yes |
| `reversal_reason` | `text` | Yes |
| `reversed_by_user_id` | `uuid` | Yes |

States:

- `ACTIVE`;
- `REVERSED`.

Constraints:

~~~sql
UNIQUE (scan_attempt_id)
CHECK (status IN ('ACTIVE','REVERSED'))

CHECK (
  (status = 'REVERSED')
  =
  (reversed_at IS NOT NULL
   AND reversal_reason IS NOT NULL
   AND reversed_by_user_id IS NOT NULL)
)
~~~

Critical single-entry invariant:

~~~sql
CREATE UNIQUE INDEX admissions_one_active_per_ticket_uq
ON admissions(ticket_entitlement_id)
WHERE status = 'ACTIVE';
~~~

This is the database-level final safeguard for concurrent duplicate scans.

Admission rows are never deleted to "undo" gate history.

---

## Part XI: AUDIT & OUTBOX

## 56. `audit_events`

Append-only material history.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `event_id` | `uuid` | Yes |
| `partner_id` | `uuid` | Yes |
| `actor_kind` | `text` | No |
| `actor_user_id` | `uuid` | Yes |
| `actor_partner_id` | `uuid` | Yes |
| `actor_buyer_session_id` | `uuid` | Yes |
| `system_actor` | `text` | Yes |
| `operation` | `text` | No |
| `entity_type` | `text` | No |
| `entity_id` | `uuid` | Yes |
| `reservation_id` | `uuid` | Yes |
| `sale_id` | `uuid` | Yes |
| `ticket_entitlement_id` | `uuid` | Yes |
| `previous_state` | `jsonb` | Yes |
| `new_state` | `jsonb` | Yes |
| `reason` | `text` | Yes |
| `idempotency_key_hash` | `bytea` | Yes |
| `correlation_id` | `uuid` | Yes |
| `metadata` | `jsonb` | No |
| `occurred_at` | `timestamptz` | No |

Actor kinds:

- `USER`;
- `PARTNER`;
- `BUYER_SESSION`;
- `SYSTEM`.

Actor exclusive-arc CHECK SHALL ensure the correct actor reference is populated for the selected kind.

Privileged actions MUST populate `reason`.

Append-only enforcement:

- application database role has no `UPDATE` or `DELETE` permission;
- a protective trigger SHOULD reject ordinary `UPDATE`/`DELETE` even if accidentally attempted by application code.

Indexes:

~~~sql
INDEX audit_events_event_time_idx (event_id, occurred_at DESC)
INDEX audit_events_entity_idx (entity_type, entity_id, occurred_at DESC)
INDEX audit_events_reservation_idx (reservation_id, occurred_at DESC)
INDEX audit_events_ticket_idx (ticket_entitlement_id, occurred_at DESC)
~~~

---

## 57. `outbox_events`

Durable post-commit facts for realtime/projections/webhook fan-out.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `enqueue_sequence` | `bigint GENERATED ALWAYS AS IDENTITY` | No |
| `fact_id` | `uuid` | No |
| `event_id` | `uuid` | Yes |
| `fact_type` | `text` | No |
| `aggregate_type` | `text` | No |
| `aggregate_id` | `uuid` | Yes |
| `payload` | `jsonb` | No |
| `created_at` | `timestamptz` | No |
| `processed_at` | `timestamptz` | Yes |
| `attempt_count` | `integer` | No |
| `next_attempt_at` | `timestamptz` | Yes |
| `last_error` | `text` | Yes |

Constraints:

~~~sql
UNIQUE (enqueue_sequence)
UNIQUE (fact_id)
CHECK (attempt_count >= 0)
~~~

Dispatcher index:

~~~sql
CREATE INDEX outbox_pending_idx
ON outbox_events(next_attempt_at, enqueue_sequence)
WHERE processed_at IS NULL;
~~~

`enqueue_sequence` is internal dispatcher metadata only. It MUST NOT be exposed as a gap-free public replay cursor because sequence allocation can precede transaction commit and concurrent transactions may become visible out of allocation order.

`processed_at` means the outbox dispatcher has established/attempted the configured fan-out for the immutable fact. It does **not** mean every browser or Partner endpoint received the event.

Per-Partner webhook delivery state is stored in `webhook_deliveries`.

Immutable fields after insert:

- `fact_id`;
- `event_id`;
- `fact_type`;
- `aggregate_type`;
- `aggregate_id`;
- `payload`;
- `created_at`.

Only dispatcher metadata may change.

### 57.1 `partner_webhook_endpoints`

Partner-owned outbound notification destination.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `partner_id` | `uuid` | No |
| `url` | `text` | No |
| `state` | `text` | No |
| `created_at` | `timestamptz` | No |
| `updated_at` | `timestamptz` | No |
| `disabled_at` | `timestamptz` | Yes |

Constraints:

~~~sql
CHECK (state IN ('ACTIVE','DISABLED'))
UNIQUE (id, partner_id)
~~~

Production URL safety (HTTPS, no private/internal destinations, no redirects) is enforced by the Security & Authentication Specification and application validation.

### 57.2 `partner_webhook_signing_secrets`

Versioned recoverable signing-secret history for one endpoint.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `webhook_endpoint_id` | `uuid` | No |
| `secret_ciphertext` | `bytea` | No |
| `encryption_key_version` | `integer` | No |
| `state` | `text` | No |
| `activated_at` | `timestamptz` | No |
| `valid_until` | `timestamptz` | Yes |
| `created_at` | `timestamptz` | No |

States:

- `ACTIVE`;
- `RETIRING`;
- `REVOKED`.

Constraints:

~~~sql
CHECK (encryption_key_version > 0)
CHECK (state IN ('ACTIVE','RETIRING','REVOKED'))
~~~

Critical partial unique index:

~~~sql
CREATE UNIQUE INDEX webhook_one_active_secret_uq
ON partner_webhook_signing_secrets(webhook_endpoint_id)
WHERE state = 'ACTIVE';
~~~

Signing secrets are encrypted at rest because TktSync must recover them to sign deliveries.

### 57.3 `partner_webhook_subscriptions`

Explicit event-type subscriptions for one endpoint.

| Column | Type | Null |
|---|---|---:|
| `webhook_endpoint_id` | `uuid` | No |
| `event_type` | `text` | No |
| `created_at` | `timestamptz` | No |

Primary key:

~~~sql
PRIMARY KEY (webhook_endpoint_id, event_type)
~~~

Unknown/unapproved event types MUST be rejected by application validation.

### 57.4 `webhook_deliveries`

Durable logical delivery of one outbox fact to one Partner endpoint.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `webhook_endpoint_id` | `uuid` | No |
| `outbox_event_id` | `uuid` | No |
| `state` | `text` | No |
| `attempt_count` | `integer` | No |
| `next_attempt_at` | `timestamptz` | Yes |
| `last_status_code` | `integer` | Yes |
| `last_error` | `text` | Yes |
| `created_at` | `timestamptz` | No |
| `delivered_at` | `timestamptz` | Yes |
| `dead_lettered_at` | `timestamptz` | Yes |

States:

- `PENDING`;
- `DELIVERED`;
- `DEAD_LETTER`;
- `CANCELLED`.

Constraints:

~~~sql
UNIQUE (webhook_endpoint_id, outbox_event_id)
CHECK (attempt_count >= 0)
CHECK (state IN ('PENDING','DELIVERED','DEAD_LETTER','CANCELLED'))
~~~

Retry index:

~~~sql
CREATE INDEX webhook_deliveries_pending_idx
ON webhook_deliveries(next_attempt_at, id)
WHERE state = 'PENDING';
~~~

`id` is the stable Delivery ID across physical HTTP retry attempts.

### 57.5 `webhook_delivery_attempts`

Optional append-preserved operational attempt history.

| Column | Type | Null |
|---|---|---:|
| `id` | `uuid` | No |
| `webhook_delivery_id` | `uuid` | No |
| `attempt_number` | `integer` | No |
| `attempted_at` | `timestamptz` | No |
| `duration_ms` | `integer` | Yes |
| `status_code` | `integer` | Yes |
| `error_class` | `text` | Yes |
| `response_excerpt` | `text` | Yes |

Constraints:

~~~sql
UNIQUE (webhook_delivery_id, attempt_number)
CHECK (attempt_number > 0)
CHECK (duration_ms IS NULL OR duration_ms >= 0)
~~~

Response excerpts MUST be bounded/sanitized and MUST NOT become a repository for Partner secrets or customer PII.

---

## Part XII: DERIVED VIEWS & REPORTING

## 58. `v_reserved_inventory_current_state`

Derived current reserved-seat disposition.

The view SHALL expose at least:

- ReservedInventoryUnit;
- Event;
- Section;
- effective current disposition;
- current claim identity/type;
- effective price tier/display price.

The view MUST NOT expose private Reservation/customer metadata to unauthorized Partner queries.

---

## 59. `v_ga_inventory_current_summary`

Derived GA summary by pool.

It SHALL aggregate:

- shared available;
- blocked current;
- allocated available;
- active reserved;
- sold current;
- issued current;
- capacity.

The result MUST satisfy pool-balance semantics.

---

## 60. Caller-Contextual Availability

Partner availability SHALL be constructed from authoritative base tables/views using caller context.

For a Partner:

- shared `AVAILABLE` reserved units are eligible;
- active channel `ALLOCATION` units assigned to that Partner are eligible;
- shared GA available quantity is eligible;
- active channel GA allocation available quantity assigned to that Partner is eligible.

Another Partner's allocation must not become visible as acquirable inventory.

Availability views are read models, never ownership.

---

## 61. Reporting

Reporting queries MUST retain semantic distinction between:

- shared available;
- active reserved;
- blocked;
- allocated but unconsumed;
- commercial `SOLD`;
- non-public `ISSUED`;
- voided ticket entitlement;
- current capacity consumption;
- historical Sale quantities;
- active Admission;
- reversed Admission;
- rejected/duplicate ScanAttempts.

Expired or released Reservations MUST NOT appear as sold.

---

## 62. Accreditation Export Support

Accreditation export may derive from:

- Event;
- Allocation purpose;
- NonPublicIssuance;
- TicketEntitlement;
- optional `ticket_attendee_details`;
- Admission where relevant.

Exports are snapshots only and MUST NOT mutate these tables.

---

## Part XIII: CROSS-TABLE CONSTRAINT TRIGGERS

## 63. Why Constraint Triggers Are Required

Some invariants cannot be expressed using a normal row-local PostgreSQL `CHECK`, including:

- GA pool capacity equals the sum of several child buckets;
- a current ReservedInventoryClaim source object actually represents the same seat and valid state;
- a TicketEntitlement inventory identity matches its Sale/Issuance origin;
- live Event physical inventory identity cannot be rewritten after protected history;
- allocation release destinations cannot form cycles.

These invariants SHALL use database constraint triggers in addition to application guards.

Constraint triggers are defense in depth; they do not replace canonical transactional lock ordering.

---

## 64. Required Constraint/Protection Triggers

### 64.1 `ct_validate_ga_pool_balance`

Deferrable, initially deferred.

Validates Section 37.

### 64.2 `ct_validate_reserved_claim`

Deferrable, initially deferred.

Validates Section 52.

### 64.3 `ct_validate_ticket_origin`

Validates:

- exactly one origin;
- Event match;
- inventory kind match;
- reserved seat or GA pool matches origin item.

### 64.4 `ct_validate_sale_item_snapshot`

On SaleItem insert, verifies correspondence to the active confirmed ReservationItem.

### 64.5 `ct_validate_issuance_item_source`

Verifies each issuance item belongs to the issuance Allocation and exact allocation membership/bucket.

### 64.6 `ct_protect_published_layout`

Rejects material modification/deletion of published/retired VenueLayoutVersion physical identity.

### 64.7 `ct_protect_live_event_inventory_identity`

Rejects physical identity changes to Event sections, reserved inventory units, GA pool identity, or finalized snapshot after protected business history.

### 64.8 `ct_validate_allocation_release_destination`

Ensures:

- same Event;
- destination is eligible;
- no self-reference;
- no recursive cycle.

### 64.9 `ct_enforce_transaction_currency`

Ensures:

- ReservationItem currency equals its Reservation currency;
- selected EventPriceTier currency equals Reservation currency;
- Sale currency equals Reservation currency;
- SaleItem currency equals Sale/Reservation currency.

No Event-wide currency assumption is introduced.

### 64.10 `ct_validate_qr_ticket_state`

Deferrable, initially deferred.

Ensures at commit:

- an `ACTIVE` QRCredential belongs to an `ACTIVE` TicketEntitlement;
- a `VOIDED` TicketEntitlement has no `ACTIVE` QRCredential;
- QRCredential rotation preserves the one-active-credential partial unique invariant.

This makes Ticket void and credential revocation one enforceable committed outcome.

### 64.11 `ct_validate_ga_active_reservations`

Deferrable, initially deferred.

Enforces Section 37.1.

### 64.12 `ct_validate_origin_ticket_cardinality`

Deferrable, initially deferred.

For every commercial SaleItem:

~~~text
COUNT(TicketEntitlement
      WHERE origin_sale_item_id = SaleItem.id
        AND replaces_ticket_entitlement_id IS NULL)
=
SaleItem.quantity
~~~

For every NonPublicIssuanceItem:

~~~text
COUNT(TicketEntitlement
      WHERE origin_issuance_item_id = IssuanceItem.id
        AND replaces_ticket_entitlement_id IS NULL)
=
IssuanceItem.quantity
~~~

Replacement tickets are not root entitlements and therefore do not change initial issuance cardinality.

This constraint guarantees:

- reserved quantity `1` creates exactly one initial ticket;
- GA quantity `N` creates exactly `N` independently admissible initial tickets.

### 64.13 `ct_validate_layout_component_scope`

Ensures every Venue layout Row/Table/Seat/GA Zone belongs to the same VenueLayoutVersion/Section scope referenced by the row.

### 64.14 `ct_validate_event_snapshot_materialization`

Ensures, where a source Venue object is present:

- EventSection source section belongs to the Event snapshot's source VenueLayoutVersion;
- ReservedInventoryUnit source seat belongs to that same source version and expected section;
- GAInventoryPool source GA zone belongs to that same source version and expected section.

This trigger prevents cross-version floor-plan identity corruption during Event inventory materialization.

### 64.15 `ct_prevent_immutable_fact_update`

Rejects prohibited updates to immutable business fact columns in:

- Sales;
- SaleItems;
- NonPublicIssuances;
- NonPublicIssuanceItems;
- historical claim identity/source fields;
- AuditEvents.

---

## Part XIV: INDEXING STRATEGY

## 65. Reserved Inventory

~~~sql
CREATE INDEX reserved_inventory_event_section_idx
ON reserved_inventory_units(event_id, event_section_id, id);

CREATE UNIQUE INDEX reserved_inventory_one_active_claim_uq
ON reserved_inventory_claims(reserved_inventory_unit_id)
WHERE ended_at IS NULL;
~~~

Source-claim indexes:

~~~sql
INDEX reserved_claim_reservation_item_idx (reservation_item_id)
WHERE reservation_item_id IS NOT NULL

INDEX reserved_claim_sale_item_idx (sale_item_id)
WHERE sale_item_id IS NOT NULL
~~~

---

## 66. GA

~~~sql
INDEX ga_inventory_event_section_idx
ON ga_inventory_pools(event_id, event_section_id, id);

INDEX ga_allocation_buckets_pool_idx
ON ga_allocation_buckets(ga_pool_id, allocation_id);

INDEX ga_block_buckets_pool_idx
ON ga_block_buckets(ga_pool_id, block_id);
~~~

---

## 67. Reservations

~~~sql
INDEX reservations_event_state_idx
(event_id, state, created_at)

INDEX reservations_partner_event_state_idx
(partner_id, event_id, state, created_at)

INDEX reservations_buyer_session_idx
(buyer_selection_session_id)
WHERE buyer_selection_session_id IS NOT NULL

UNIQUE INDEX reservations_continuation_token_uq
(continuation_token_hash)
~~~

Deadline indexes are defined in Section 40.

---

## 68. Partner/Allocation

~~~sql
INDEX partner_event_access_event_idx
(event_id, partner_id, state)

INDEX restrictions_event_state_idx
(event_id, kind, state)

INDEX allocations_partner_idx
(partner_id, restriction_id)
WHERE partner_id IS NOT NULL

INDEX allocation_reserved_units_unit_idx
(reserved_inventory_unit_id, allocation_id)
~~~

---

## 69. Tickets & Admission

~~~sql
INDEX ticket_event_status_idx
(event_id, status)

UNIQUE INDEX ticket_one_active_reserved_unit_uq
(reserved_inventory_unit_id)
WHERE inventory_kind = 'RESERVED' AND status = 'ACTIVE'

UNIQUE INDEX qr_credentials_token_hash_uq
(token_hash)

UNIQUE INDEX qr_credentials_one_active_uq
(ticket_entitlement_id)
WHERE status = 'ACTIVE'

INDEX scan_attempts_event_time_idx
(event_id, occurred_at DESC)

UNIQUE INDEX admissions_one_active_per_ticket_uq
(ticket_entitlement_id)
WHERE status = 'ACTIVE'
~~~

---

## 70. Index Review Rule

Every additional index on a high-write table MUST be justified by:

- an authoritative command lookup;
- a worker candidate query;
- a critical read path;
- a constraint requirement.

Indexes are not added merely because a column appears in filters.

This limits write amplification on high-contention tables.

---

## Part XV: FOREIGN-KEY & DELETION POLICY

## 71. General FK Rule

Historical business references SHALL normally use:

~~~sql
ON DELETE RESTRICT
~~~

or default `NO ACTION`.

`ON DELETE CASCADE` is reserved for structural draft/configuration children whose parent itself cannot be deleted once protected history exists.

---

## 72. Safe Cascade Areas

Typical safe structural cascades:

- unused draft VenueLayoutVersion -> its draft layout children;
- deletable draft Event -> EventLayoutSnapshot/EventSection/EventTransactionPolicy/event-specific inventory, provided no business-history FK exists.

The application MUST still enforce upstream hard-deletion rules.

---

## 73. Business History Restriction

The following parent objects MUST be `RESTRICT`ed by historical children:

- Event referenced by Reservation/Sale/Ticket/Admission/Audit;
- Partner referenced by Reservation/Sale;
- Reservation referenced by Sale;
- Sale referenced by SaleItems/Tickets;
- NonPublicIssuance referenced by issuance items/tickets;
- Ticket referenced by credential/admission history.

Routine deletion must not orphan history.

---

## Part XVI: CANONICAL FOREIGN-KEY MATRIX

## 74. Required Foreign Keys and Delete Actions

The following foreign keys are normative. Composite scope keys SHOULD be used where indicated so cross-Event or cross-Partner references cannot be created accidentally.

| Child column(s) | Parent | Delete |
|---|---|---|
| `venue_layout_versions.venue_id` | `venues.id` | `RESTRICT` |
| layout child `layout_version_id` | `venue_layout_versions.id` | `CASCADE` subject to published-layout protection |
| layout row/table/seat `section_id` | `venue_layout_sections.id` | `RESTRICT` |
| layout seat `row_id` | `venue_layout_rows.id` | `RESTRICT` |
| layout seat `table_id` | `venue_layout_tables.id` | `RESTRICT` |
| `events.venue_id` | `venues.id` | `RESTRICT` |
| `event_transaction_policies.event_id` | `events.id` | `CASCADE` only while Event deletion is domain-legal |
| `event_layout_snapshots.event_id` | `events.id` | `CASCADE` only while Event deletion is domain-legal |
| `event_layout_snapshots.source_layout_version_id` | `venue_layout_versions.id` | `RESTRICT` |
| `event_sections.event_id` | `events.id` | `CASCADE` only while Event deletion is domain-legal |
| `event_sections.source_layout_section_id` | `venue_layout_sections.id` | `RESTRICT` |
| `event_price_tiers.event_id` | `events.id` | `CASCADE` only while Event deletion is domain-legal |
| `event_sections.(default_price_tier_id,event_id)` | `event_price_tiers.(id,event_id)` | `RESTRICT` |
| `reserved_inventory_units.(event_section_id,event_id)` | `event_sections.(id,event_id)` | `RESTRICT` |
| `reserved_inventory_units.source_venue_seat_id` | `venue_layout_seats.id` | `RESTRICT` |
| `reserved_inventory_units.(price_tier_override_id,event_id)` | `event_price_tiers.(id,event_id)` | `RESTRICT` |
| `ga_inventory_pools.(event_section_id,event_id)` | `event_sections.(id,event_id)` | `RESTRICT` |
| `ga_inventory_pools.source_ga_zone_id` | `venue_layout_ga_zones.id` | `RESTRICT` |
| `ga_inventory_pools.(price_tier_id,event_id)` | `event_price_tiers.(id,event_id)` | `RESTRICT` |
| `ga_shared_inventory.ga_pool_id` | `ga_inventory_pools.id` | `CASCADE` only while pool deletion is domain-legal |
| `platform_user_roles.user_id` | `app_users.id` | `RESTRICT` |
| `event_staff_assignments.event_id` | `events.id` | `RESTRICT` once historical usage exists |
| `event_staff_assignments.user_id` | `app_users.id` | `RESTRICT` |
| `partner_credentials.partner_id` | `partners.id` | `RESTRICT` |
| `partner_event_access.partner_id` | `partners.id` | `RESTRICT` |
| `partner_event_access.event_id` | `events.id` | `RESTRICT` |
| `buyer_selection_sessions.partner_id` | `partners.id` | `RESTRICT` |
| `buyer_selection_sessions.event_id` | `events.id` | `RESTRICT` |
| `inventory_restrictions.event_id` | `events.id` | `RESTRICT` |
| `inventory_restrictions.created_by_user_id` | `app_users.id` | `RESTRICT` |
| `blocks.restriction_id` | `inventory_restrictions.id` | `RESTRICT` |
| `allocations.restriction_id` | `inventory_restrictions.id` | `RESTRICT` |
| `allocations.partner_id` | `partners.id` | `RESTRICT` |
| `allocations.release_destination_allocation_id` | `allocations.restriction_id` | `RESTRICT` |
| `block_reserved_units.block_id` | `blocks.restriction_id` | `RESTRICT` |
| `block_reserved_units.reserved_inventory_unit_id` | `reserved_inventory_units.id` | `RESTRICT` |
| `allocation_reserved_units.allocation_id` | `allocations.restriction_id` | `RESTRICT` |
| `allocation_reserved_units.reserved_inventory_unit_id` | `reserved_inventory_units.id` | `RESTRICT` |
| `ga_block_buckets.block_id` | `blocks.restriction_id` | `RESTRICT` |
| `ga_block_buckets.ga_pool_id` | `ga_inventory_pools.id` | `RESTRICT` |
| `ga_allocation_buckets.allocation_id` | `allocations.restriction_id` | `RESTRICT` |
| `ga_allocation_buckets.ga_pool_id` | `ga_inventory_pools.id` | `RESTRICT` |
| `reservations.event_id` | `events.id` | `RESTRICT` |
| `reservations.partner_id` | `partners.id` | `RESTRICT` |
| `reservations.(buyer_selection_session_id,partner_id,event_id)` | `buyer_selection_sessions.(id,partner_id,event_id)` | `RESTRICT` |
| `reservation_items.(reservation_id,event_id)` | `reservations.(id,event_id)` | `RESTRICT` |
| `reservation_items.(reserved_inventory_unit_id,event_id)` | `reserved_inventory_units.(id,event_id)` | `RESTRICT` |
| `reservation_items.(ga_pool_id,event_id)` | `ga_inventory_pools.(id,event_id)` | `RESTRICT` |
| `reservation_items.source_allocation_reserved_unit_id` | `allocation_reserved_units.id` | `RESTRICT` |
| `reservation_items.source_ga_allocation_bucket_id` | `ga_allocation_buckets.id` | `RESTRICT` |
| `reservation_items.(price_tier_id,event_id)` | `event_price_tiers.(id,event_id)` | `RESTRICT` |
| `checkout_attempts.reservation_id` | `reservations.id` | `RESTRICT` |
| `sales.(reservation_id,event_id,partner_id)` | `reservations.(id,event_id,partner_id)` | `RESTRICT` |
| `sale_items.sale_id` | `sales.id` | `RESTRICT` |
| `sale_items.reservation_item_id` | `reservation_items.id` | `RESTRICT` |
| `sale_items.(reserved_inventory_unit_id,event_id)` | `reserved_inventory_units.(id,event_id)` | `RESTRICT` |
| `sale_items.(ga_pool_id,event_id)` | `ga_inventory_pools.(id,event_id)` | `RESTRICT` |
| `non_public_issuances.event_id` | `events.id` | `RESTRICT` |
| `non_public_issuances.allocation_id` | `allocations.restriction_id` | `RESTRICT` |
| `non_public_issuances.issued_by_user_id` | `app_users.id` | `RESTRICT` |
| `non_public_issuance_items.issuance_id` | `non_public_issuances.id` | `RESTRICT` |
| `non_public_issuance_items.reserved_inventory_unit_id` | `reserved_inventory_units.id` | `RESTRICT` |
| `non_public_issuance_items.ga_pool_id` | `ga_inventory_pools.id` | `RESTRICT` |
| `non_public_issuance_items.allocation_reserved_unit_id` | `allocation_reserved_units.id` | `RESTRICT` |
| `non_public_issuance_items.ga_allocation_bucket_id` | `ga_allocation_buckets.id` | `RESTRICT` |
| `ticket_entitlements.origin_sale_item_id` | `sale_items.id` | `RESTRICT` |
| `ticket_entitlements.origin_issuance_item_id` | `non_public_issuance_items.id` | `RESTRICT` |
| `ticket_entitlements.replaces_ticket_entitlement_id` | `ticket_entitlements.id` | `RESTRICT` |
| `ticket_entitlements.reserved_inventory_unit_id` | `reserved_inventory_units.id` | `RESTRICT` |
| `ticket_entitlements.ga_pool_id` | `ga_inventory_pools.id` | `RESTRICT` |
| `ticket_attendee_details.ticket_entitlement_id` | `ticket_entitlements.id` | `RESTRICT` |
| `qr_credentials.ticket_entitlement_id` | `ticket_entitlements.id` | `RESTRICT` |
| `reserved_inventory_claims.reserved_inventory_unit_id` | `reserved_inventory_units.id` | `RESTRICT` |
| claim source FKs | respective Reservation/Block/Allocation/Sale/Issuance item | `RESTRICT` |
| idempotency actor FKs | Partner/User/BuyerSelectionSession | `RESTRICT` |
| `scan_attempts.event_id` | `events.id` | `RESTRICT` |
| `scan_attempts.scanner_user_id` | `app_users.id` | `RESTRICT` |
| `scan_attempts.ticket_entitlement_id` | `ticket_entitlements.id` | `RESTRICT` |
| `scan_attempts.qr_credential_id` | `qr_credentials.id` | `RESTRICT` |
| `scan_attempts.idempotency_operation_id` | `idempotency_operations.id` | `RESTRICT` |
| `admissions.event_id` | `events.id` | `RESTRICT` |
| `admissions.ticket_entitlement_id` | `ticket_entitlements.id` | `RESTRICT` |
| `admissions.scan_attempt_id` | `scan_attempts.id` | `RESTRICT` |
| `admissions.reversed_by_user_id` | `app_users.id` | `RESTRICT` |
| audit correlation FKs, where present | Event/Partner/User/Reservation/Sale/Ticket | `RESTRICT` or deliberately denormalized immutable reference |
| `outbox_events.event_id` | `events.id` | `RESTRICT` where non-null |
| `partner_webhook_endpoints.partner_id` | `partners.id` | `RESTRICT` |
| `partner_webhook_signing_secrets.webhook_endpoint_id` | `partner_webhook_endpoints.id` | `RESTRICT` |
| `partner_webhook_subscriptions.webhook_endpoint_id` | `partner_webhook_endpoints.id` | `CASCADE` only for never-used draft endpoint; otherwise explicit cleanup/disable |
| `webhook_deliveries.webhook_endpoint_id` | `partner_webhook_endpoints.id` | `RESTRICT` |
| `webhook_deliveries.outbox_event_id` | `outbox_events.id` | `RESTRICT` |
| `webhook_delivery_attempts.webhook_delivery_id` | `webhook_deliveries.id` | `RESTRICT` |

Cross-scope composite foreign keys are preferred over independent FKs when both sides already carry the same Event/Partner scope. This makes cross-tenant/cross-event corruption impossible at the relational layer rather than merely unlikely.

---

## Part XVII: IMMUTABILITY RULES

## 75. Immutable After Publication / Protection

### Venue/Layout
After layout publication:

- physical object IDs;
- structural component membership;
- geometry snapshot

cannot be materially rewritten.

### Event Inventory
After protected history:

- Event snapshot physical identity;
- ReservedInventoryUnit stable physical mapping;
- GA pool identity

cannot be silently rewritten.

Presentation labels MAY change only where upstream policy considers the change non-destructive.

---

## 76. Immutable After Reservation Protection

When Reservation enters `COMMITTING`:

- active ReservationItem composition;
- source allocation identity;
- quantity;
- commercial terms snapshot

are frozen.

---

## 77. Immutable Facts

Ordinary application code MUST NOT mutate the business meaning of:

- Sale;
- SaleItem;
- NonPublicIssuance;
- NonPublicIssuanceItem;
- original Ticket origin;
- historical ScanAttempt;
- historical AuditEvent.

Ticket status, credential status, current inventory claim, and Admission status may evolve through their explicit domain workflows without rewriting origin history.

---

## Part XVII: ROW LOCK TARGETS

## 78. Canonical Lock Rows by Command

The schema SHALL support the System Architecture lock hierarchy.

| Command | Primary lock rows |
|---|---|
| Create hold | Event, Partner/Access, source Allocation, ReservedInventoryUnit(s), GA Pool(s), GA source buckets |
| Modify hold | Event, Reservation, source Allocations, old/new ReservedInventoryUnit(s), GA Pool(s)/buckets |
| Begin checkout | Event, Reservation |
| Payment failure | Event, Reservation, CheckoutAttempt |
| Release/expiry | Event, Reservation, source Allocations, inventory units/pools/buckets |
| Confirm | Event, Reservation, source Allocations, inventory units/pools/buckets |
| Block/allocation | Event, Restriction where existing, inventory units/pools |
| Non-public issuance | Event, Allocation, inventory units/pools/buckets |
| Cancel Event | Event |
| Void ticket | Event, Ticket, active QRCredential |
| Credential reissue | Event, Ticket, active QRCredential |
| Admit | Event, Ticket, presented QRCredential, active Admission check |

The `reserved_inventory_units` row remains the stable reserved-seat lock target even though current disposition lives in the claim table.

The `ga_inventory_pools` row remains the GA aggregate lock target even though accounting is distributed across source buckets.

---

## Part XVIII: MIGRATION ORDER

## 79. Canonical Initial Migration Sequence

A safe initial creation order is:

1. PostgreSQL extensions/helper functions;
2. `app_users`;
3. `platform_user_roles`;
4. `venues`;
5. Venue layout tables;
6. `partners`;
7. `partner_credentials`;
8. `events`;
9. `event_transaction_policies`;
10. `event_layout_snapshots`;
11. `event_price_tiers`;
12. `event_sections`;
13. `reserved_inventory_units`;
14. `ga_inventory_pools`;
15. `ga_shared_inventory`;
16. `event_staff_assignments`;
17. `partner_event_access`;
18. `buyer_selection_sessions`;
19. `inventory_restrictions`;
20. `blocks`;
21. `allocations`;
22. restriction reserved memberships;
23. GA restriction/allocation buckets;
24. `idempotency_operations`;
25. `reservations`;
26. `reservation_items`;
27. `checkout_attempts`;
28. `sales`;
29. `sale_items`;
30. `non_public_issuances`;
31. `non_public_issuance_items`;
32. `ticket_entitlements`;
33. `ticket_attendee_details`;
34. `qr_credentials`;
35. `reserved_inventory_claims`;
36. `scan_attempts`;
37. `admissions`;
38. `audit_events`;
39. `outbox_events`;
40. `partner_webhook_endpoints`;
41. `partner_webhook_signing_secrets`;
42. `partner_webhook_subscriptions`;
43. `webhook_deliveries`;
44. `webhook_delivery_attempts`;
45. indexes;
46. constraint/protection triggers;
47. derived views;
48. database grants/RLS-defense rules.

Some composite foreign keys may be added in a second migration after all referenced tables exist.

---

## 80. Migration Discipline

All migrations MUST be:

- version-controlled;
- deterministic;
- reviewed;
- reproducible in a clean database;
- safe against existing historical data.

Production code MUST NOT create or alter authoritative tables ad hoc at runtime.

---

## Part XIX: SECURITY AT THE DATABASE LAYER

## 81. Database Roles

At minimum, deployment SHOULD distinguish:

- migration/owner role;
- Core API application role;
- worker role;
- outbox dispatcher role;
- read/reporting role where needed.

Anonymous browser clients MUST NOT receive direct write authority over authoritative tables.

---

## 82. RLS / Supabase Exposure

Because authoritative web applications operate through the Core API, Row Level Security is defense in depth rather than the primary domain authorization mechanism.

If any table is exposed through Supabase client access or Realtime authorization:

- RLS MUST be enabled;
- policies MUST not expose Partner-private Reservation/order/payment data;
- direct client writes to authoritative inventory tables MUST remain disabled.

Realtime SHOULD publish sanitized outbox-derived facts rather than granting broad direct table subscriptions.

---

## 83. Secret Material

The database may persist only digests/non-reversible storage for:

- Partner API secret material;
- BuyerSelectionSession capability tokens;
- Reservation continuation/hold tokens;
- QR credential secrets.

Raw values are returned only at issuance time where the applicable protocol requires it.

Partner webhook signing secrets are a deliberate exception to one-way storage because TktSync must recover them to generate outbound HMAC signatures. They SHALL be stored only as authenticated ciphertext with an external encryption-key version, as defined by [Security and Authentication](security.md).

---

## Part XX: VALIDATION & CONCURRENCY TESTS

## 84. Reserved Seat Double-Hold

100 concurrent holds of one seat:

Expected database state:

- one active `RESERVATION` claim;
- one owning ReservationItem;
- 99 non-owning requests;
- no second active claim.

---

## 85. Mixed Hold Atomicity

Request reserved A12 + GA quantity 3.

If either cannot be acquired:

- no active ReservedInventoryClaim for the new Reservation;
- no GA bucket quantity moved;
- no partial Reservation remains.

---

## 86. GA Contention

Pool current shared availability = 10.

100 concurrent requests quantity 1:

- exactly 10 units move from `available_quantity` to `active_reserved_quantity`;
- balance trigger passes;
- no negative counter.

---

## 87. Allocation-Sourced Reservation Release

Allocated seat held then released while Allocation active:

- Reservation claim ends;
- active `ALLOCATION` claim is restored;
- seat never becomes shared available.

---

## 88. Allocation Released During Hold

Allocation released while buyer Reservation remains protected:

- Reservation claim remains active;
- allocation membership persists;
- Allocation records release destination;
- later Reservation release creates destination claim or shared availability according to recorded release rule;
- no inventory is lost or duplicated.

GA equivalent:

- Allocation bucket active reserved quantity remains;
- unreserved quantity moves to destination and `released_quantity`;
- later hold release moves reserved quantity to destination and `released_quantity`.

---

## 89. Confirmation

Successful confirmation must commit:

- Reservation `CONFIRMED`;
- Sale;
- SaleItems;
- Reserved `SALE` claim(s) or GA sold counters;
- TicketEntitlements;
- active QRCredentials;
- AuditEvents;
- OutboxEvents;
- idempotency success.

Any transaction failure rolls back all of them.

---

## 90. Ticket Void Without Re-release

After Ticket void:

- Ticket `VOIDED`;
- active QR `REVOKED`;
- Sale unchanged;
- Reserved `SALE` claim remains active or GA `sold_current_quantity` remains consumed;
- no availability increase.

---

## 91. Explicit Re-release

After valid re-release:

Reserved:

- `SALE`/`ISSUANCE` claim ends;
- destination becomes no active claim (`AVAILABLE`) or active `ALLOCATION` claim.

GA:

- origin current sold/issued quantity decreases;
- destination current available quantity increases;
- historical source bucket `released_quantity` increases when leaving that source;
- pool balance remains exact.

---

## 92. Duplicate Scan

100 distinct scan requests of one single-entry active ticket:

- one ScanAttempt = `ADMITTED`;
- one active Admission;
- remaining ScanAttempts = `ALREADY_ADMITTED`;
- partial unique Admission index prevents a second winner.

---

## 93. Scan Retry

Same scan idempotency key after response loss:

- same idempotency row;
- same ScanAttempt;
- original `ADMITTED` result replayed;
- no false duplicate classification.

### 93.1 Ticket Cardinality Validation

For a reserved SaleItem with `quantity = 1`, committing zero or two root TicketEntitlements MUST fail.

For a GA SaleItem with `quantity = 4`, committing any root TicketEntitlement count other than four MUST fail.

A later replacement Ticket with `replaces_ticket_entitlement_id IS NOT NULL` MUST NOT change the root-cardinality result.

---

## 94. Worker Delay

Expired Reservation row still physically `HELD` because worker is stopped:

- BeginCheckout evaluates effective time and rejects;
- no new protected right is created;
- worker later materializes `EXPIRED`;
- inventory source is restored safely.

---

## 95. GA Balance Constraint Failure Test

A deliberately malformed test transaction that changes an allocation bucket without balancing the pool MUST fail at commit through `ct_validate_ga_pool_balance`.

---

## 96. Reserved Claim Constraint Failure Test

A deliberately malformed test transaction attempting two active claims on one seat MUST fail through `reserved_inventory_one_active_claim_uq`.

---

## Part XXI: TRACEABILITY TO GOVERNING DOCUMENTS

## 97. Technical Brief Alignment

The schema supports every data requirement in the original Technical Brief:

| Brief capability | Schema support |
|---|---|
| Single real-time inventory truth | PostgreSQL authoritative inventory rows/claims/buckets |
| General Admission | GA pools + shared/allocation/block buckets |
| Reserved seating | event-specific ReservedInventoryUnits + claim history |
| Mixed inventory | ReservationItems support reserved and GA in one Reservation |
| Floor-plan builder | versioned geometry JSON + normalized sections/rows/tables/seats/GA zones |
| Sections/zones | Venue and Event section tables |
| Rows/seats/tables | normalized layout tables |
| Standing capacity | GA pool capacity |
| Stage/ring/field orientation | layout geometry/metadata |
| Section/seat pricing | EventPriceTier + section default + seat override |
| Sponsor/VIP/media/comp/fighter/production blocks | restriction/allocation model |
| Realtime updates | OutboxEvents |
| Partner availability | contextual reads from shared/eligible allocation state |
| Atomic hold | Reservation + claim/bucket transaction |
| Opaque hold token | `reservations.continuation_token_hash` + raw one-time issuance to caller |
| Hold expiry | Reservation deadlines/indexes |
| Confirm/release | Sale / source-aware restoration |
| White-label selector | BuyerSelectionSession |
| Ticket ID + QR | TicketEntitlement + QRCredential |
| Duplicate scan prevention | ScanAttempt + active Admission unique index |
| Audit log | AuditEvents |
| Partner reporting | Partner/Event-linked Reservation/Sale/reportable state |
| Accreditation export | Allocation/Issuance/Ticket/attendee extension |
| Supabase row locking | schema designed around PostgreSQL lock targets |

The schema does not add payment-processing tables, CRM, messaging, dynamic pricing, native mobile state, or enterprise analytics because those remain outside the assessment MVP.

---

## 98. Platform Policy Alignment

| Policy requirement | Schema enforcement |
|---|---|
| One inventory truth | one PostgreSQL authority |
| Availability not ownership | derived views only |
| Reserved one state | one active ReservedInventoryClaim |
| GA never negative | nonnegative bucket checks + deferred exact pool balance + active Reservation reconciliation |
| Multi-item all-or-nothing | one Reservation transaction; no partial schema state survives rollback |
| No silent substitution | immutable ReservationItem inventory identity |
| Hold ownership | Reservation Partner/Event/session scope + hashed continuation token that is not standalone authority |
| Price snapshot | ReservationItem commercial columns |
| Hold expiry bounded | deadline columns + max lifetime |
| Protected checkout | CheckoutAttempt + Reservation state |
| Payment retry/reconciliation | explicit Reservation states/deadlines |
| Late confirm cannot reclaim | terminal Reservation + active inventory source checks |
| Idempotency | IdempotencyOperations unique scope |
| Block cannot steal hold | active claim uniqueness |
| Allocation issuance distinct | NonPublicIssuance, not Sale |
| GA capacity reduction guarded | pool lock + balance trigger |
| Partner neutrality | shared state has no Partner priority |
| Channel allocation explicit | Allocation mode + Partner FK |
| Partner disable vs credential revoke | independent tables/states |
| Ticket/QR separate | TicketEntitlement and QRCredential |
| Scan idempotent and unique admission | IdempotencyOperation + active Admission partial unique |
| Void separate from resale | ticket state does not automatically change claim/bucket |
| Audit append-only | AuditEvents privileges/triggers |
| Reports derived | views/queries only |
| PII minimized | optional attendee table separated from inventory |

---

## 99. Logical Domain Specification Alignment

The schema preserves every independent state dimension:

| Domain dimension | Relational location |
|---|---|
| Venue layout lifecycle | `venue_layout_versions.state` |
| Event lifecycle | `events.state` |
| Reservation lifecycle | `reservations.state` |
| Reserved inventory disposition | active `reserved_inventory_claims.claim_type` |
| GA accounting | shared + block + allocation buckets under GA pool |
| Restriction lifecycle | `inventory_restrictions.state` |
| Allocation mode | `allocations.mode` |
| Ticket entitlement | `ticket_entitlements.status` + root/replacement lineage |
| QR credential | `qr_credentials.status` |
| Scan attempt | `scan_attempts.result` |
| Admission | `admissions.status` |
| Partner account | `partners.state` |
| Partner credential | `partner_credentials.state` |
| Partner Event access | `partner_event_access.state` |

No overloaded status column merges unrelated lifecycle dimensions.

---

## 100. System Architecture Alignment

| Architecture requirement | Schema mechanism |
|---|---|
| PostgreSQL authority | all irreversible state persisted here |
| READ COMMITTED + row locks | stable Event, Reservation, seat, GA pool lock rows |
| canonical lock ordering | schema has explicit lock targets and source references |
| Event lifecycle gate | `events` row |
| Partner acquisition gate | `partners` + `partner_event_access` |
| server-authoritative deadlines | `timestamptz` deadline columns |
| worker not authoritative | deadline indexes are discovery only |
| durable idempotency | `idempotency_operations` |
| transactional outbox | `outbox_events` |
| no distributed lock | database uniqueness/locks provide ownership |
| confirmation one transaction | Reservation/Sale/Ticket/claim/bucket rows share DB |
| admission one transaction | ScanAttempt/Admission share DB |
| no external call in transaction | no external-state FK dependency |
| fail closed | authoritative write requires primary DB state |
| database constraints as final safeguards | partial uniques, checks, deferred triggers |

---

## Part XXII: REVIEW FINDINGS & DRIFT RESOLUTIONS

## 101. Review Resolution: Reserved Current State

**Rejected design:** multiple nullable `current_*` FKs stored directly on `reserved_inventory_units`.

**Reason rejected:**

- circular FK relationships with Sale/Issuance;
- difficult historical resale representation;
- many invalid null combinations;
- duplicated current-state history.

**Final design:** append-preserved `reserved_inventory_claims` plus one-active-claim partial unique index.

This is fully aligned with the domain concept of one current claim.

---

## 102. Review Resolution: GA Accounting

**Rejected design:** duplicate aggregate counters on GA pool plus separate Allocation counters with no database reconciliation.

**Reason rejected:** the same quantity would be represented authoritatively in two places and could drift.

**Final design:**

- GA pool as capacity/lock root;
- shared source row;
- block buckets;
- allocation buckets;
- deferred exact pool-balance trigger.

This preserves source-aware release and makes current capacity mathematically reconcilable.

---

## 103. Review Resolution: Allocation Release During Active Reservation

The schema explicitly preserves:

- allocation membership history;
- Allocation release destination;
- ReservationItem source;
- active reserved bucket quantity.

An administrative Allocation release therefore cannot destroy buyer ownership or accidentally release the same inventory twice.

---

## 104. Review Resolution: Commercial Sale vs Non-Public Issuance

Commercial and non-commercial origins remain physically separate:

- `sales` / `sale_items`;
- `non_public_issuances` / `non_public_issuance_items`.

TicketEntitlement has exactly one origin.

No zero-value fake Sale is created for comp/VIP/media issuance.

---

## 105. Review Resolution: Ticket Void vs Inventory Re-release

Ticket void updates:

- TicketEntitlement;
- QRCredential.

It does not update:

- Sale history;
- current reserved SALE claim;
- GA sold/issued capacity.

Re-release is a separate transaction.

This prevents accidental resale merely because a refund/void occurred.

---

## 106. Review Resolution: Admission Retry vs Duplicate Admission

`scan_attempts` and `admissions` remain separate.

Idempotency identifies the same technical scan operation.

The active Admission unique index identifies distinct attempts competing for the same entitlement.

Therefore:

- retry of same successful scan => same success;
- distinct later scan => already admitted.

---

## 107. Review Resolution: Realtime Revision and Outbox Ordering

The schema does **not** maintain an Event `inventory_revision` counter updated on every hold.

That design would create an avoidable Event-row write hotspot.

The outbox `enqueue_sequence` is retained only as internal dispatcher metadata. It is **not** a public replay cursor or strict commit-order guarantee because sequence allocation can occur before transaction commit and concurrent transactions may become visible out of allocation order.

Browser clients resynchronize from authoritative state after interruption. Partner webhook consumers deduplicate using immutable event IDs and recover current state through the API.

---

## 108. Review Resolution: Currency

**Rejected design:** one mandatory currency stored on the Event.

**Reason rejected:** the upstream documents define pricing tiers but do not state that an Event must be single-currency.

**Final design:**

- EventPriceTier carries currency;
- Reservation carries one transaction currency;
- all active ReservationItems must match that currency;
- Sale/SaleItems preserve the same confirmed transaction currency.

This prevents incoherent mixed-currency checkout without inventing an Event-wide product restriction that the governing documents never stated.

---

## 109. Review Resolution: Allocation Pricing

**Rejected design:** an Allocation-specific `price_tier_id` override.

**Reason rejected:** upstream policy/domain documents define Event-controlled pricing on sections, reserved units, and GA pools. Allocation changes eligibility/source, not commercial pricing semantics.

**Final design:** channel-allocated inventory uses the effective Event price of the underlying reserved unit or GA pool. ReservationItems snapshot the resolved terms at acquisition.

---

## 110. Review Resolution: Floor-Plan Library Independence

Visual geometry is stored as snapshot JSONB, while business seat/section/GA identities are normalized relational rows.

The external seat-map library is therefore replaceable without changing authoritative inventory identity.

The third-party library MUST NOT become the inventory source of truth.

---

## Part XXIII: NON-NEGOTIABLE DATABASE INVARIANTS

## 111. Database Invariants

1. One Event-specific reserved inventory identity represents one physical sellable unit.
2. A reserved inventory unit has at most one active claim.
3. No active claim means the reserved unit is available to the relevant contextual rules.
4. GA current bucket quantities always sum exactly to capacity.
5. No GA quantity is negative.
6. A Reservation cannot contain an item from another Event.
7. A ReservationItem's inventory identity is never silently changed.
8. Active ReservationItems preserve their acquisition source.
9. Reservation commercial snapshots are preserved.
10. At most one active CheckoutAttempt exists per Reservation.
11. A confirmed Reservation has exactly one Sale.
12. One ReservationItem may produce at most one SaleItem.
13. A NonPublicIssuance is never a Sale.
14. A TicketEntitlement has exactly one origin.
15. A reserved physical inventory unit has at most one `ACTIVE` TicketEntitlement at a time; historical voided/replacement/resale entitlements may coexist.
16. Each SaleItem/NonPublicIssuanceItem has exactly `quantity` root TicketEntitlements; later replacements do not alter root cardinality.
17. At most one active QRCredential exists per TicketEntitlement, and an active credential cannot belong to a voided Ticket.
18. At most one active Admission exists per single-entry TicketEntitlement.
19. Ticket void does not delete or mutate Sale/Issuance history.
20. Ticket void does not automatically free inventory.
21. Historical ReservedInventoryClaims are not deleted to hide state transitions.
22. AuditEvents are append-only.
23. Idempotency operation scope is unique.
24. Deterministic business retries can replay stable outcomes.
25. Realtime Outbox facts are post-commit records.
26. Outbox delivery metadata may change; the fact payload may not.
27. Published Venue layout identity is immutable.
28. Live Event inventory physical identity is immutable.
29. Partner credentials and Partner account state are independent.
30. BuyerSelectionSession credentials do not grant Partner authority.
31. Reservation continuation/hold tokens are stored only as digests and do not grant mutation authority by themselves.
32. No customer PII is required to establish inventory ownership.
33. Database inability prevents new authoritative ownership changes.
34. All schema-level enforcement remains subordinate to canonical transactional lock ordering.
---

## Part XXIV: IMPLEMENTATION HANDOFF

## 112. ORM / Query-Layer Requirements

The implementation query layer MUST support the relational model without flattening it into weaker abstractions.

It MUST support:

- explicit transactions;
- row locks;
- raw/parameterized SQL for critical paths;
- partial unique constraints;
- compound foreign keys;
- deferrable trigger-compatible migrations;
- PostgreSQL errors as recognizable business/consistency failures.

ORM convenience MUST NOT result in read-modify-write inventory updates outside required locks.

---

## 113. API Specification Handoff

The subsequent API & Partner Integration Contract SHALL map:

- public IDs;
- Reservation status;
- hold token/capability behavior;
- availability responses;
- Partner order/payment references;
- business errors

onto this schema without exposing internal tables as the API contract.

Table names are not endpoint names.

---

## 114. Realtime Contract Handoff

The subsequent Realtime/Event Contract SHALL derive publishable facts from `outbox_events`.

It MUST define:

- sanitized payloads;
- Partner/Event authorization;
- revision/sequence behavior;
- reconnect/resync rules.

It MUST NOT expose private raw Reservation records simply because Supabase can stream table changes.

---

## 115. Security Specification Handoff

The subsequent Security/Auth specification SHALL define exact cryptographic formats for:

- Partner secrets;
- buyer capability tokens;
- QR tokens;
- token hashing;
- secret rotation.

This schema only fixes storage and scope semantics.

---

## 116. No Unresolved Relational Policy Gap

The final review found no remaining upstream policy or logical-domain requirement that lacks a relational representation or explicitly assigned enforcement mechanism.

Items intentionally deferred are implementation-contract details, not unresolved business semantics:

- exact API routes and JSON;
- exact cryptographic algorithm/token encoding;
- exact third-party floor-plan library;
- rate-limit numeric values;
- retention durations;
- UI representation;
- realtime transport channel naming.

These deferred details MUST not change the relational invariants in this specification.

---

## 117. Final Relational Model Summary

~~~text
VENUE
  └── VENUE LAYOUT VERSION
       ├── SECTIONS / ROWS / TABLES / SEATS
       └── GA ZONES
               │
               ▼ snapshot/materialize
EVENT ───────────────────────────────────────────────┐
  ├── EVENT POLICY                                  │
  ├── EVENT LAYOUT SNAPSHOT                         │
  ├── PRICE TIERS                                   │
  ├── RESERVED INVENTORY UNITS                      │
  │       └── RESERVED INVENTORY CLAIM HISTORY      │
  │               ├── RESERVATION                   │
  │               ├── BLOCK                         │
  │               ├── ALLOCATION                    │
  │               ├── SALE                          │
  │               └── ISSUANCE                      │
  │                                                 │
  └── GA INVENTORY POOLS                            │
          ├── SHARED INVENTORY                      │
          ├── BLOCK BUCKETS                         │
          └── ALLOCATION BUCKETS                    │
                                                    │
PARTNER ── PARTNER EVENT ACCESS                     │
   │                                                │
   └── RESERVATION ── RESERVATION ITEMS ────────────┘
          └── CHECKOUT ATTEMPTS
                  │
                  ▼
                SALE ── SALE ITEMS
                  │
                  ▼
           TICKET ENTITLEMENT
                  │
                  └── QR CREDENTIAL HISTORY
                           │
                           ▼
                      SCAN ATTEMPT
                           │
                           ▼
                        ADMISSION

NON-PUBLIC ALLOCATION
      └── NON-PUBLIC ISSUANCE
              └── TICKET ENTITLEMENT

EVERY MATERIAL MUTATION
      ├── IDEMPOTENCY OPERATION
      ├── AUDIT EVENT
      └── OUTBOX EVENT
~~~

The relational governing principle is:

> **Stable identities are normalized; current ownership is represented once; historical facts are preserved; quantity accounting balances at commit; and every invariant that can be enforced by PostgreSQL is enforced by PostgreSQL rather than assumed by application convention.**

---

**End of Document**

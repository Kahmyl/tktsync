BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================================
-- VENUE & LAYOUT
-- ============================================================================

CREATE TABLE venues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    address_text text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX venues_name_idx ON venues(name);

CREATE TABLE venue_layout_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    version_number integer NOT NULL,
    state text NOT NULL,
    geometry_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    content_hash bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    retired_at timestamptz,

    CONSTRAINT venue_layout_versions_state_ck
        CHECK (state IN ('DRAFT','PUBLISHED','RETIRED')),
    CONSTRAINT venue_layout_versions_version_ck
        CHECK (version_number > 0),
    CONSTRAINT venue_layout_versions_venue_version_uq
        UNIQUE (venue_id, version_number),
    CONSTRAINT venue_layout_versions_id_venue_uq
        UNIQUE (id, venue_id)
);

CREATE TABLE venue_layout_sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE CASCADE,
    object_key text NOT NULL,
    name text NOT NULL,
    section_kind text NOT NULL,
    sort_order integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT venue_layout_sections_kind_ck
        CHECK (section_kind IN ('RESERVED','GA','TABLE','MIXED_VISUAL')),
    CONSTRAINT venue_layout_sections_object_uq
        UNIQUE (layout_version_id, object_key),
    CONSTRAINT venue_layout_sections_id_layout_uq
        UNIQUE (id, layout_version_id)
);

CREATE TABLE venue_layout_rows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE CASCADE,
    section_id uuid NOT NULL,
    object_key text NOT NULL,
    label text NOT NULL,
    sort_order integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT venue_layout_rows_object_uq
        UNIQUE (layout_version_id, object_key),
    CONSTRAINT venue_layout_rows_id_layout_uq
        UNIQUE (id, layout_version_id),

    CONSTRAINT venue_layout_rows_section_fk
        FOREIGN KEY (section_id, layout_version_id)
        REFERENCES venue_layout_sections(id, layout_version_id)
        ON DELETE RESTRICT
);

CREATE TABLE venue_layout_tables (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE CASCADE,
    section_id uuid NOT NULL,
    object_key text NOT NULL,
    label text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT venue_layout_tables_object_uq
        UNIQUE (layout_version_id, object_key),
    CONSTRAINT venue_layout_tables_id_layout_uq
        UNIQUE (id, layout_version_id),

    CONSTRAINT venue_layout_tables_section_fk
        FOREIGN KEY (section_id, layout_version_id)
        REFERENCES venue_layout_sections(id, layout_version_id)
        ON DELETE RESTRICT
);

CREATE TABLE venue_layout_seats (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE CASCADE,
    section_id uuid NOT NULL,
    row_id uuid,
    table_id uuid,
    object_key text NOT NULL,
    seat_label text NOT NULL,
    sort_order integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT venue_layout_seats_object_uq
        UNIQUE (layout_version_id, object_key),
    CONSTRAINT venue_layout_seats_id_layout_uq
        UNIQUE (id, layout_version_id),

    CONSTRAINT venue_layout_seats_section_fk
        FOREIGN KEY (section_id, layout_version_id)
        REFERENCES venue_layout_sections(id, layout_version_id)
        ON DELETE RESTRICT,

    CONSTRAINT venue_layout_seats_row_fk
        FOREIGN KEY (row_id, layout_version_id)
        REFERENCES venue_layout_rows(id, layout_version_id)
        ON DELETE RESTRICT,

    CONSTRAINT venue_layout_seats_table_fk
        FOREIGN KEY (table_id, layout_version_id)
        REFERENCES venue_layout_tables(id, layout_version_id)
        ON DELETE RESTRICT
);

CREATE TABLE venue_layout_ga_zones (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE CASCADE,
    section_id uuid NOT NULL,
    object_key text NOT NULL,
    name text NOT NULL,
    default_capacity integer,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT venue_layout_ga_zones_object_uq
        UNIQUE (layout_version_id, object_key),
    CONSTRAINT venue_layout_ga_zones_id_layout_uq
        UNIQUE (id, layout_version_id),
    CONSTRAINT venue_layout_ga_zones_capacity_ck
        CHECK (default_capacity IS NULL OR default_capacity >= 0),

    CONSTRAINT venue_layout_ga_zones_section_fk
        FOREIGN KEY (section_id, layout_version_id)
        REFERENCES venue_layout_sections(id, layout_version_id)
        ON DELETE RESTRICT
);

-- ============================================================================
-- USERS & PARTNERS
-- ============================================================================

CREATE TABLE app_users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_provider text NOT NULL,
    auth_subject text NOT NULL,
    display_name text,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT app_users_provider_subject_uq
        UNIQUE (auth_provider, auth_subject),
    CONSTRAINT app_users_state_ck
        CHECK (state IN ('ACTIVE','DISABLED'))
);

CREATE TABLE platform_user_roles (
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE RESTRICT,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, role),

    CONSTRAINT platform_user_roles_role_ck
        CHECK (role IN ('PLATFORM_ADMIN'))
);

CREATE TABLE partners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    state text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,

    CONSTRAINT partners_state_ck
        CHECK (state IN ('ACTIVE','DISABLED'))
);

CREATE TABLE partner_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES partners(id) ON DELETE RESTRICT,
    key_id text NOT NULL,
    secret_hash bytea NOT NULL,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz,

    CONSTRAINT partner_credentials_key_id_uq UNIQUE (key_id),
    CONSTRAINT partner_credentials_state_ck
        CHECK (state IN ('ACTIVE','REVOKED'))
);

-- ============================================================================
-- EVENT & INVENTORY
-- ============================================================================

CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id uuid NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    name text NOT NULL,
    state text NOT NULL,
    starts_at timestamptz,
    ends_at timestamptz,
    sales_open_at timestamptz,
    sales_close_at timestamptz,
    admission_open_at timestamptz,
    admission_close_at timestamptz,
    timezone_name text,
    admission_policy text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    cancelled_at timestamptz,
    completed_at timestamptz,

    CONSTRAINT events_state_ck CHECK (
        state IN (
            'DRAFT','ON_SALE','PAUSED',
            'SALES_CLOSED','COMPLETED','CANCELLED'
        )
    ),
    CONSTRAINT events_admission_policy_ck
        CHECK (admission_policy IN ('SINGLE_ENTRY')),
    CONSTRAINT events_time_ck
        CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at),
    CONSTRAINT events_sales_time_ck
        CHECK (
            sales_close_at IS NULL OR
            sales_open_at IS NULL OR
            sales_close_at >= sales_open_at
        ),
    CONSTRAINT events_admission_time_ck
        CHECK (
            admission_close_at IS NULL OR
            admission_open_at IS NULL OR
            admission_close_at >= admission_open_at
        )
);

CREATE TABLE event_transaction_policies (
    event_id uuid PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    hold_duration_seconds integer NOT NULL,
    checkout_protection_seconds integer NOT NULL,
    payment_retry_seconds integer NOT NULL,
    reconciliation_seconds integer NOT NULL,
    max_reservation_lifetime_seconds integer NOT NULL,
    max_hold_quantity integer NOT NULL,
    max_active_reservations_per_partner integer NOT NULL,
    max_active_reservations_per_buyer_session integer NOT NULL,
    allow_voided_inventory_rerelease boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT event_policy_hold_ck CHECK (hold_duration_seconds > 0),
    CONSTRAINT event_policy_checkout_ck CHECK (checkout_protection_seconds > 0),
    CONSTRAINT event_policy_retry_ck CHECK (payment_retry_seconds >= 0),
    CONSTRAINT event_policy_reconcile_ck CHECK (reconciliation_seconds > 0),
    CONSTRAINT event_policy_lifetime_ck
        CHECK (max_reservation_lifetime_seconds >= hold_duration_seconds),
    CONSTRAINT event_policy_hold_quantity_ck CHECK (max_hold_quantity > 0),
    CONSTRAINT event_policy_partner_active_ck
        CHECK (max_active_reservations_per_partner > 0),
    CONSTRAINT event_policy_buyer_active_ck
        CHECK (max_active_reservations_per_buyer_session > 0)
);

CREATE TABLE event_layout_snapshots (
    event_id uuid PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    source_layout_version_id uuid NOT NULL
        REFERENCES venue_layout_versions(id) ON DELETE RESTRICT,
    snapshot_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    content_hash bytea,
    finalized_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- NOTE:
-- The Markdown column table accidentally omitted currency while its own
-- constraints and final currency resolution require EventPriceTier.currency.
CREATE TABLE event_price_tiers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    code text NOT NULL,
    name text NOT NULL,
    amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT event_price_tiers_code_uq UNIQUE (event_id, code),
    CONSTRAINT event_price_tiers_id_event_uq UNIQUE (id, event_id),
    CONSTRAINT event_price_tiers_amount_ck CHECK (amount_minor >= 0),
    CONSTRAINT event_price_tiers_currency_ck
        CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT event_price_tiers_state_ck
        CHECK (state IN ('ACTIVE','RETIRED'))
);

CREATE TABLE event_sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    source_layout_section_id uuid
        REFERENCES venue_layout_sections(id) ON DELETE RESTRICT,
    snapshot_object_key text NOT NULL,
    name text NOT NULL,
    default_price_tier_id uuid,
    sort_order integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT event_sections_object_uq
        UNIQUE (event_id, snapshot_object_key),
    CONSTRAINT event_sections_id_event_uq
        UNIQUE (id, event_id),

    CONSTRAINT event_sections_price_tier_fk
        FOREIGN KEY (default_price_tier_id, event_id)
        REFERENCES event_price_tiers(id, event_id)
        ON DELETE RESTRICT
);

CREATE TABLE reserved_inventory_units (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    event_section_id uuid NOT NULL,
    source_venue_seat_id uuid
        REFERENCES venue_layout_seats(id) ON DELETE RESTRICT,
    snapshot_object_key text NOT NULL,
    row_label text,
    seat_label text NOT NULL,
    table_label text,
    display_label text NOT NULL,
    price_tier_override_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT reserved_inventory_units_object_uq
        UNIQUE (event_id, snapshot_object_key),
    CONSTRAINT reserved_inventory_units_id_event_uq
        UNIQUE (id, event_id),

    CONSTRAINT reserved_inventory_units_section_fk
        FOREIGN KEY (event_section_id, event_id)
        REFERENCES event_sections(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT reserved_inventory_units_price_tier_fk
        FOREIGN KEY (price_tier_override_id, event_id)
        REFERENCES event_price_tiers(id, event_id)
        ON DELETE RESTRICT
);

-- NOTE:
-- price_tier_id is required by the effective-pricing rules and canonical FK
-- matrix even though the Markdown column table omitted the row.
CREATE TABLE ga_inventory_pools (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    event_section_id uuid NOT NULL,
    source_ga_zone_id uuid
        REFERENCES venue_layout_ga_zones(id) ON DELETE RESTRICT,
    snapshot_object_key text NOT NULL,
    name text NOT NULL,
    capacity integer NOT NULL,
    price_tier_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ga_inventory_pools_object_uq
        UNIQUE (event_id, snapshot_object_key),
    CONSTRAINT ga_inventory_pools_id_event_uq
        UNIQUE (id, event_id),
    CONSTRAINT ga_inventory_pools_capacity_ck
        CHECK (capacity >= 0),

    CONSTRAINT ga_inventory_pools_section_fk
        FOREIGN KEY (event_section_id, event_id)
        REFERENCES event_sections(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT ga_inventory_pools_price_tier_fk
        FOREIGN KEY (price_tier_id, event_id)
        REFERENCES event_price_tiers(id, event_id)
        ON DELETE RESTRICT
);

CREATE TABLE ga_shared_inventory (
    ga_pool_id uuid PRIMARY KEY
        REFERENCES ga_inventory_pools(id) ON DELETE CASCADE,
    available_quantity integer NOT NULL,
    active_reserved_quantity integer NOT NULL,
    sold_current_quantity integer NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ga_shared_available_ck CHECK (available_quantity >= 0),
    CONSTRAINT ga_shared_reserved_ck CHECK (active_reserved_quantity >= 0),
    CONSTRAINT ga_shared_sold_ck CHECK (sold_current_quantity >= 0)
);

CREATE TABLE event_staff_assignments (
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE RESTRICT,
    role text NOT NULL,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,

    PRIMARY KEY (event_id, user_id, role),

    CONSTRAINT event_staff_role_ck CHECK (
        role IN (
            'EVENT_MANAGER','BOX_OFFICE','GATE_SUPERVISOR',
            'SCANNER','VIEWER'
        )
    ),
    CONSTRAINT event_staff_state_ck
        CHECK (state IN ('ACTIVE','DISABLED'))
);

CREATE TABLE partner_event_access (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES partners(id) ON DELETE RESTRICT,
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,

    CONSTRAINT partner_event_access_partner_event_uq
        UNIQUE (partner_id, event_id),
    CONSTRAINT partner_event_access_id_scope_uq
        UNIQUE (id, partner_id, event_id),
    CONSTRAINT partner_event_access_state_ck
        CHECK (state IN ('ACTIVE','DISABLED'))
);

CREATE TABLE buyer_selection_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES partners(id) ON DELETE RESTRICT,
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    token_hash bytea NOT NULL,
    token_key_version integer NOT NULL,
    buyer_session_ref text,
    state text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,

    CONSTRAINT buyer_selection_sessions_token_uq
        UNIQUE (token_hash),
    CONSTRAINT buyer_selection_sessions_id_scope_uq
        UNIQUE (id, partner_id, event_id),
    CONSTRAINT buyer_selection_sessions_key_version_ck
        CHECK (token_key_version > 0),
    CONSTRAINT buyer_selection_sessions_state_ck
        CHECK (state IN ('ACTIVE','REVOKED','EXPIRED'))
);

-- ============================================================================
-- RESTRICTIONS & ALLOCATIONS
-- ============================================================================

CREATE TABLE inventory_restrictions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    kind text NOT NULL,
    state text NOT NULL,
    purpose text NOT NULL,
    reason text,
    created_by_user_id uuid NOT NULL
        REFERENCES app_users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    closed_at timestamptz,

    CONSTRAINT inventory_restrictions_kind_ck
        CHECK (kind IN ('BLOCK','ALLOCATION')),
    CONSTRAINT inventory_restrictions_state_ck
        CHECK (state IN ('ACTIVE','RELEASED','CLOSED')),
    CONSTRAINT inventory_restrictions_id_event_uq
        UNIQUE (id, event_id)
);

CREATE TABLE blocks (
    restriction_id uuid PRIMARY KEY
        REFERENCES inventory_restrictions(id) ON DELETE RESTRICT
);

CREATE TABLE allocations (
    restriction_id uuid PRIMARY KEY
        REFERENCES inventory_restrictions(id) ON DELETE RESTRICT,
    mode text NOT NULL,
    partner_id uuid REFERENCES partners(id) ON DELETE RESTRICT,
    release_destination_kind text NOT NULL,
    release_destination_allocation_id uuid
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT,

    CONSTRAINT allocations_mode_ck
        CHECK (mode IN ('CHANNEL','NON_PUBLIC')),
    CONSTRAINT allocations_destination_kind_ck
        CHECK (release_destination_kind IN ('SHARED','ALLOCATION')),
    CONSTRAINT allocations_partner_mode_ck CHECK (
        (mode = 'CHANNEL' AND partner_id IS NOT NULL)
        OR
        (mode = 'NON_PUBLIC' AND partner_id IS NULL)
    ),
    CONSTRAINT allocations_destination_ck CHECK (
        (
            release_destination_kind = 'SHARED'
            AND release_destination_allocation_id IS NULL
        )
        OR
        (
            release_destination_kind = 'ALLOCATION'
            AND release_destination_allocation_id IS NOT NULL
        )
    ),
    CONSTRAINT allocations_no_self_destination_ck CHECK (
        release_destination_allocation_id IS NULL
        OR release_destination_allocation_id <> restriction_id
    )
);

CREATE TABLE block_reserved_units (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    block_id uuid NOT NULL REFERENCES blocks(restriction_id) ON DELETE RESTRICT,
    reserved_inventory_unit_id uuid NOT NULL
        REFERENCES reserved_inventory_units(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,

    CONSTRAINT block_reserved_units_membership_uq
        UNIQUE (block_id, reserved_inventory_unit_id),
    CONSTRAINT block_reserved_units_id_unit_uq
        UNIQUE (id, reserved_inventory_unit_id)
);

CREATE TABLE allocation_reserved_units (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    allocation_id uuid NOT NULL
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT,
    reserved_inventory_unit_id uuid NOT NULL
        REFERENCES reserved_inventory_units(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,

    CONSTRAINT allocation_reserved_units_membership_uq
        UNIQUE (allocation_id, reserved_inventory_unit_id),
    CONSTRAINT allocation_reserved_units_id_unit_uq
        UNIQUE (id, reserved_inventory_unit_id)
);

CREATE TABLE ga_block_buckets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    block_id uuid NOT NULL REFERENCES blocks(restriction_id) ON DELETE RESTRICT,
    ga_pool_id uuid NOT NULL
        REFERENCES ga_inventory_pools(id) ON DELETE RESTRICT,
    assigned_quantity integer NOT NULL,
    blocked_quantity integer NOT NULL,
    released_quantity integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ga_block_buckets_scope_uq
        UNIQUE (block_id, ga_pool_id),
    CONSTRAINT ga_block_buckets_id_pool_uq
        UNIQUE (id, ga_pool_id),
    CONSTRAINT ga_block_buckets_assigned_ck CHECK (assigned_quantity >= 0),
    CONSTRAINT ga_block_buckets_blocked_ck CHECK (blocked_quantity >= 0),
    CONSTRAINT ga_block_buckets_released_ck CHECK (released_quantity >= 0),
    CONSTRAINT ga_block_buckets_balance_ck
        CHECK (assigned_quantity = blocked_quantity + released_quantity)
);

CREATE TABLE ga_allocation_buckets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    allocation_id uuid NOT NULL
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT,
    ga_pool_id uuid NOT NULL
        REFERENCES ga_inventory_pools(id) ON DELETE RESTRICT,
    assigned_quantity integer NOT NULL,
    available_quantity integer NOT NULL,
    active_reserved_quantity integer NOT NULL,
    sold_current_quantity integer NOT NULL,
    issued_current_quantity integer NOT NULL,
    released_quantity integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ga_allocation_buckets_scope_uq
        UNIQUE (allocation_id, ga_pool_id),
    CONSTRAINT ga_allocation_buckets_id_pool_uq
        UNIQUE (id, ga_pool_id),
    CONSTRAINT ga_allocation_buckets_assigned_ck CHECK (assigned_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_available_ck CHECK (available_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_reserved_ck
        CHECK (active_reserved_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_sold_ck CHECK (sold_current_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_issued_ck CHECK (issued_current_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_released_ck CHECK (released_quantity >= 0),
    CONSTRAINT ga_allocation_buckets_balance_ck CHECK (
        assigned_quantity =
            available_quantity
            + active_reserved_quantity
            + sold_current_quantity
            + issued_current_quantity
            + released_quantity
    )
);

-- ============================================================================
-- IDEMPOTENCY
-- ============================================================================

CREATE TABLE idempotency_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_kind text NOT NULL,
    partner_id uuid REFERENCES partners(id) ON DELETE RESTRICT,
    app_user_id uuid REFERENCES app_users(id) ON DELETE RESTRICT,
    buyer_selection_session_id uuid
        REFERENCES buyer_selection_sessions(id) ON DELETE RESTRICT,
    operation_type text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL,
    execution_state text NOT NULL,
    result_code text,
    result_entity_type text,
    result_entity_id uuid,
    result_payload jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,

    CONSTRAINT idempotency_actor_count_ck CHECK (
        num_nonnulls(
            partner_id,
            app_user_id,
            buyer_selection_session_id
        ) = 1
    ),
    CONSTRAINT idempotency_scope_actor_ck CHECK (
        (scope_kind = 'PARTNER' AND partner_id IS NOT NULL)
        OR
        (scope_kind = 'USER' AND app_user_id IS NOT NULL)
        OR
        (
            scope_kind = 'BUYER_SESSION'
            AND buyer_selection_session_id IS NOT NULL
        )
    ),
    CONSTRAINT idempotency_scope_kind_ck
        CHECK (scope_kind IN ('PARTNER','USER','BUYER_SESSION')),
    CONSTRAINT idempotency_execution_state_ck CHECK (
        execution_state IN ('IN_PROGRESS','SUCCEEDED','FAILED_BUSINESS')
    )
);

-- ============================================================================
-- RESERVATION & CHECKOUT
-- ============================================================================

CREATE TABLE reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    partner_id uuid NOT NULL REFERENCES partners(id) ON DELETE RESTRICT,
    buyer_selection_session_id uuid,
    partner_customer_ref text,
    partner_order_ref text,
    buyer_session_ref text,
    continuation_token_hash bytea NOT NULL,
    continuation_token_key_version integer NOT NULL,
    currency char(3) NOT NULL,
    state text NOT NULL,
    hold_expires_at timestamptz NOT NULL,
    payment_retry_expires_at timestamptz,
    reconciliation_expires_at timestamptz,
    max_lifetime_at timestamptz NOT NULL,
    terminal_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz,
    released_at timestamptz,
    expired_at timestamptz,

    CONSTRAINT reservations_state_ck CHECK (
        state IN (
            'HELD','COMMITTING','PAYMENT_RETRY','RECONCILING',
            'CONFIRMED','RELEASED','EXPIRED'
        )
    ),
    CONSTRAINT reservations_currency_ck
        CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT reservations_hold_deadline_ck
        CHECK (hold_expires_at <= max_lifetime_at),
    CONSTRAINT reservations_retry_deadline_ck CHECK (
        payment_retry_expires_at IS NULL
        OR payment_retry_expires_at <= max_lifetime_at
    ),
    CONSTRAINT reservations_reconcile_deadline_ck CHECK (
        reconciliation_expires_at IS NULL
        OR reconciliation_expires_at <= max_lifetime_at
    ),
    CONSTRAINT reservations_confirmed_timestamp_ck CHECK (
        (state = 'CONFIRMED') = (confirmed_at IS NOT NULL)
    ),
    CONSTRAINT reservations_released_timestamp_ck CHECK (
        (state = 'RELEASED') = (released_at IS NOT NULL)
    ),
    CONSTRAINT reservations_expired_timestamp_ck CHECK (
        (state = 'EXPIRED') = (expired_at IS NOT NULL)
    ),
    CONSTRAINT reservations_terminal_reason_ck CHECK (
        state NOT IN ('RELEASED','EXPIRED')
        OR terminal_reason IS NOT NULL
    ),
    CONSTRAINT reservations_id_event_uq UNIQUE (id, event_id),
    CONSTRAINT reservations_id_event_partner_uq
        UNIQUE (id, event_id, partner_id),
    CONSTRAINT reservations_continuation_token_uq
        UNIQUE (continuation_token_hash),
    CONSTRAINT reservations_token_key_version_ck
        CHECK (continuation_token_key_version > 0),

    CONSTRAINT reservations_buyer_session_scope_fk
        FOREIGN KEY (
            buyer_selection_session_id,
            partner_id,
            event_id
        )
        REFERENCES buyer_selection_sessions(id, partner_id, event_id)
        ON DELETE RESTRICT
);

-- NOTE:
-- currency is required by transaction-currency enforcement and the final
-- currency resolution even though its Markdown column table omitted the row.
CREATE TABLE reservation_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    inventory_kind text NOT NULL,
    reserved_inventory_unit_id uuid,
    ga_pool_id uuid,
    quantity integer NOT NULL,
    source_kind text NOT NULL,
    source_allocation_reserved_unit_id uuid
        REFERENCES allocation_reserved_units(id) ON DELETE RESTRICT,
    source_ga_allocation_bucket_id uuid
        REFERENCES ga_allocation_buckets(id) ON DELETE RESTRICT,
    price_tier_id uuid,
    unit_amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    price_tier_label_snapshot text,
    commercial_terms jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    removed_at timestamptz,

    CONSTRAINT reservation_items_kind_ck
        CHECK (inventory_kind IN ('RESERVED','GA')),
    CONSTRAINT reservation_items_source_kind_ck
        CHECK (source_kind IN ('SHARED','ALLOCATION')),
    CONSTRAINT reservation_items_quantity_ck CHECK (quantity > 0),
    CONSTRAINT reservation_items_amount_ck CHECK (unit_amount_minor >= 0),
    CONSTRAINT reservation_items_currency_ck
        CHECK (currency ~ '^[A-Z]{3}$'),

    CONSTRAINT reservation_items_inventory_arc_ck CHECK (
        (
            inventory_kind = 'RESERVED'
            AND reserved_inventory_unit_id IS NOT NULL
            AND ga_pool_id IS NULL
            AND quantity = 1
        )
        OR
        (
            inventory_kind = 'GA'
            AND reserved_inventory_unit_id IS NULL
            AND ga_pool_id IS NOT NULL
        )
    ),

    CONSTRAINT reservation_items_source_arc_ck CHECK (
        (
            source_kind = 'SHARED'
            AND source_allocation_reserved_unit_id IS NULL
            AND source_ga_allocation_bucket_id IS NULL
        )
        OR
        (
            source_kind = 'ALLOCATION'
            AND (
                (
                    inventory_kind = 'RESERVED'
                    AND source_allocation_reserved_unit_id IS NOT NULL
                    AND source_ga_allocation_bucket_id IS NULL
                )
                OR
                (
                    inventory_kind = 'GA'
                    AND source_allocation_reserved_unit_id IS NULL
                    AND source_ga_allocation_bucket_id IS NOT NULL
                )
            )
        )
    ),

    CONSTRAINT reservation_items_reservation_scope_fk
        FOREIGN KEY (reservation_id, event_id)
        REFERENCES reservations(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT reservation_items_reserved_scope_fk
        FOREIGN KEY (reserved_inventory_unit_id, event_id)
        REFERENCES reserved_inventory_units(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT reservation_items_ga_scope_fk
        FOREIGN KEY (ga_pool_id, event_id)
        REFERENCES ga_inventory_pools(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT reservation_items_price_tier_scope_fk
        FOREIGN KEY (price_tier_id, event_id)
        REFERENCES event_price_tiers(id, event_id)
        ON DELETE RESTRICT
);

CREATE TABLE checkout_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_id uuid NOT NULL
        REFERENCES reservations(id) ON DELETE RESTRICT,
    attempt_number integer NOT NULL,
    state text NOT NULL,
    protection_expires_at timestamptz NOT NULL,
    partner_payment_ref text,
    partner_outcome_code text,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT checkout_attempts_number_uq
        UNIQUE (reservation_id, attempt_number),
    CONSTRAINT checkout_attempts_number_ck
        CHECK (attempt_number > 0),
    CONSTRAINT checkout_attempts_state_ck CHECK (
        state IN (
            'ACTIVE','PAYMENT_FAILED','UNCERTAIN',
            'CONFIRMED','ABANDONED'
        )
    )
);

-- ============================================================================
-- SALES, ISSUANCE & ENTITLEMENTS
-- ============================================================================

-- NOTE:
-- currency is required by the table's own CHECK and final currency rules.
CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    partner_id uuid NOT NULL,
    partner_order_ref text,
    partner_payment_ref text,
    currency char(3) NOT NULL,
    confirmed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sales_reservation_uq UNIQUE (reservation_id),
    CONSTRAINT sales_id_event_uq UNIQUE (id, event_id),
    CONSTRAINT sales_currency_ck
        CHECK (currency ~ '^[A-Z]{3}$'),

    CONSTRAINT sales_reservation_scope_fk
        FOREIGN KEY (reservation_id, event_id, partner_id)
        REFERENCES reservations(id, event_id, partner_id)
        ON DELETE RESTRICT
);

-- NOTE:
-- currency is required by ct_enforce_transaction_currency.
CREATE TABLE sale_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    reservation_item_id uuid NOT NULL
        REFERENCES reservation_items(id) ON DELETE RESTRICT,
    event_id uuid NOT NULL,
    inventory_kind text NOT NULL,
    reserved_inventory_unit_id uuid,
    ga_pool_id uuid,
    quantity integer NOT NULL,
    source_allocation_id uuid
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT,
    unit_amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sale_items_reservation_item_uq
        UNIQUE (reservation_item_id),
    CONSTRAINT sale_items_id_event_uq
        UNIQUE (id, event_id),
    CONSTRAINT sale_items_kind_ck
        CHECK (inventory_kind IN ('RESERVED','GA')),
    CONSTRAINT sale_items_quantity_ck CHECK (quantity > 0),
    CONSTRAINT sale_items_amount_ck CHECK (unit_amount_minor >= 0),
    CONSTRAINT sale_items_currency_ck
        CHECK (currency ~ '^[A-Z]{3}$'),

    CONSTRAINT sale_items_inventory_arc_ck CHECK (
        (
            inventory_kind = 'RESERVED'
            AND reserved_inventory_unit_id IS NOT NULL
            AND ga_pool_id IS NULL
            AND quantity = 1
        )
        OR
        (
            inventory_kind = 'GA'
            AND reserved_inventory_unit_id IS NULL
            AND ga_pool_id IS NOT NULL
        )
    ),

    CONSTRAINT sale_items_reserved_scope_fk
        FOREIGN KEY (reserved_inventory_unit_id, event_id)
        REFERENCES reserved_inventory_units(id, event_id)
        ON DELETE RESTRICT,

    CONSTRAINT sale_items_ga_scope_fk
        FOREIGN KEY (ga_pool_id, event_id)
        REFERENCES ga_inventory_pools(id, event_id)
        ON DELETE RESTRICT
);

CREATE TABLE non_public_issuances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    allocation_id uuid NOT NULL
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT,
    issued_by_user_id uuid NOT NULL
        REFERENCES app_users(id) ON DELETE RESTRICT,
    recipient_ref text,
    reason text,
    issued_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT non_public_issuances_id_event_uq
        UNIQUE (id, event_id)
);

CREATE TABLE non_public_issuance_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issuance_id uuid NOT NULL
        REFERENCES non_public_issuances(id) ON DELETE RESTRICT,
    event_id uuid NOT NULL,
    inventory_kind text NOT NULL,
    reserved_inventory_unit_id uuid
        REFERENCES reserved_inventory_units(id) ON DELETE RESTRICT,
    ga_pool_id uuid
        REFERENCES ga_inventory_pools(id) ON DELETE RESTRICT,
    allocation_reserved_unit_id uuid
        REFERENCES allocation_reserved_units(id) ON DELETE RESTRICT,
    ga_allocation_bucket_id uuid
        REFERENCES ga_allocation_buckets(id) ON DELETE RESTRICT,
    quantity integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT non_public_issuance_items_kind_ck
        CHECK (inventory_kind IN ('RESERVED','GA')),
    CONSTRAINT non_public_issuance_items_quantity_ck
        CHECK (quantity > 0),

    CONSTRAINT non_public_issuance_items_arc_ck CHECK (
        (
            inventory_kind = 'RESERVED'
            AND reserved_inventory_unit_id IS NOT NULL
            AND allocation_reserved_unit_id IS NOT NULL
            AND ga_pool_id IS NULL
            AND ga_allocation_bucket_id IS NULL
            AND quantity = 1
        )
        OR
        (
            inventory_kind = 'GA'
            AND reserved_inventory_unit_id IS NULL
            AND allocation_reserved_unit_id IS NULL
            AND ga_pool_id IS NOT NULL
            AND ga_allocation_bucket_id IS NOT NULL
        )
    )
);

CREATE TABLE ticket_entitlements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    origin_sale_item_id uuid
        REFERENCES sale_items(id) ON DELETE RESTRICT,
    origin_issuance_item_id uuid
        REFERENCES non_public_issuance_items(id) ON DELETE RESTRICT,
    replaces_ticket_entitlement_id uuid
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    inventory_kind text NOT NULL,
    reserved_inventory_unit_id uuid
        REFERENCES reserved_inventory_units(id) ON DELETE RESTRICT,
    ga_pool_id uuid
        REFERENCES ga_inventory_pools(id) ON DELETE RESTRICT,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    voided_at timestamptz,
    void_reason text,

    CONSTRAINT ticket_entitlements_origin_ck CHECK (
        num_nonnulls(origin_sale_item_id, origin_issuance_item_id) = 1
    ),
    CONSTRAINT ticket_entitlements_kind_ck
        CHECK (inventory_kind IN ('RESERVED','GA')),
    CONSTRAINT ticket_entitlements_status_ck
        CHECK (status IN ('ACTIVE','VOIDED')),
    CONSTRAINT ticket_entitlements_inventory_arc_ck CHECK (
        (
            inventory_kind = 'RESERVED'
            AND reserved_inventory_unit_id IS NOT NULL
            AND ga_pool_id IS NULL
        )
        OR
        (
            inventory_kind = 'GA'
            AND reserved_inventory_unit_id IS NULL
            AND ga_pool_id IS NOT NULL
        )
    ),
    CONSTRAINT ticket_entitlements_voided_at_ck CHECK (
        (status = 'VOIDED') = (voided_at IS NOT NULL)
    )
);

CREATE TABLE ticket_attendee_details (
    ticket_entitlement_id uuid PRIMARY KEY
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    partner_attendee_ref text,
    display_name text,
    accreditation_data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE qr_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_entitlement_id uuid NOT NULL
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    token_hash bytea NOT NULL,
    token_key_version integer NOT NULL,
    status text NOT NULL,
    issued_at timestamptz NOT NULL,
    superseded_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT qr_credentials_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT qr_credentials_key_version_ck CHECK (token_key_version > 0),
    CONSTRAINT qr_credentials_status_ck
        CHECK (status IN ('ACTIVE','SUPERSEDED','REVOKED')),
    CONSTRAINT qr_credentials_timestamps_ck CHECK (
        (
            status = 'ACTIVE'
            AND superseded_at IS NULL
            AND revoked_at IS NULL
        )
        OR
        (
            status = 'SUPERSEDED'
            AND superseded_at IS NOT NULL
            AND revoked_at IS NULL
        )
        OR
        (
            status = 'REVOKED'
            AND revoked_at IS NOT NULL
        )
    )
);

-- ============================================================================
-- RESERVED INVENTORY CLAIM HISTORY
-- ============================================================================

CREATE TABLE reserved_inventory_claims (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reserved_inventory_unit_id uuid NOT NULL
        REFERENCES reserved_inventory_units(id) ON DELETE RESTRICT,
    claim_type text NOT NULL,
    reservation_item_id uuid
        REFERENCES reservation_items(id) ON DELETE RESTRICT,
    block_reserved_unit_id uuid
        REFERENCES block_reserved_units(id) ON DELETE RESTRICT,
    allocation_reserved_unit_id uuid
        REFERENCES allocation_reserved_units(id) ON DELETE RESTRICT,
    sale_item_id uuid
        REFERENCES sale_items(id) ON DELETE RESTRICT,
    issuance_item_id uuid
        REFERENCES non_public_issuance_items(id) ON DELETE RESTRICT,
    activated_at timestamptz NOT NULL,
    ended_at timestamptz,
    end_reason text,

    CONSTRAINT reserved_inventory_claims_type_ck CHECK (
        claim_type IN (
            'RESERVATION','BLOCK','ALLOCATION','SALE','ISSUANCE'
        )
    ),

    CONSTRAINT reserved_inventory_claims_source_count_ck CHECK (
        num_nonnulls(
            reservation_item_id,
            block_reserved_unit_id,
            allocation_reserved_unit_id,
            sale_item_id,
            issuance_item_id
        ) = 1
    ),

    CONSTRAINT reserved_inventory_claims_source_type_ck CHECK (
        (
            claim_type = 'RESERVATION'
            AND reservation_item_id IS NOT NULL
        )
        OR
        (
            claim_type = 'BLOCK'
            AND block_reserved_unit_id IS NOT NULL
        )
        OR
        (
            claim_type = 'ALLOCATION'
            AND allocation_reserved_unit_id IS NOT NULL
        )
        OR
        (
            claim_type = 'SALE'
            AND sale_item_id IS NOT NULL
        )
        OR
        (
            claim_type = 'ISSUANCE'
            AND issuance_item_id IS NOT NULL
        )
    )
);

-- ============================================================================
-- ADMISSION
-- ============================================================================

CREATE TABLE scan_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    scanner_user_id uuid NOT NULL
        REFERENCES app_users(id) ON DELETE RESTRICT,
    ticket_entitlement_id uuid
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    qr_credential_id uuid
        REFERENCES qr_credentials(id) ON DELETE RESTRICT,
    idempotency_operation_id uuid NOT NULL
        REFERENCES idempotency_operations(id) ON DELETE RESTRICT,
    result text NOT NULL,
    gate_reference text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,

    CONSTRAINT scan_attempts_idempotency_uq
        UNIQUE (idempotency_operation_id),
    CONSTRAINT scan_attempts_result_ck CHECK (
        result IN (
            'ADMITTED',
            'ALREADY_ADMITTED',
            'INVALID_CREDENTIAL',
            'CREDENTIAL_REVOKED',
            'CREDENTIAL_SUPERSEDED',
            'TICKET_VOID',
            'WRONG_EVENT',
            'EVENT_CANCELLED',
            'ADMISSION_NOT_OPEN',
            'NOT_AUTHORIZED',
            'MANUAL_OVERRIDE_ADMITTED'
        )
    )
);

CREATE TABLE admissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    ticket_entitlement_id uuid NOT NULL
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    scan_attempt_id uuid NOT NULL
        REFERENCES scan_attempts(id) ON DELETE RESTRICT,
    status text NOT NULL,
    admitted_at timestamptz NOT NULL,
    reversed_at timestamptz,
    reversal_reason text,
    reversed_by_user_id uuid
        REFERENCES app_users(id) ON DELETE RESTRICT,

    CONSTRAINT admissions_scan_attempt_uq
        UNIQUE (scan_attempt_id),
    CONSTRAINT admissions_status_ck
        CHECK (status IN ('ACTIVE','REVERSED')),
    CONSTRAINT admissions_reversal_ck CHECK (
        (
            status = 'ACTIVE'
            AND reversed_at IS NULL
            AND reversal_reason IS NULL
            AND reversed_by_user_id IS NULL
        )
        OR
        (
            status = 'REVERSED'
            AND reversed_at IS NOT NULL
            AND reversal_reason IS NOT NULL
            AND reversed_by_user_id IS NOT NULL
        )
    )
);

-- ============================================================================
-- AUDIT & OUTBOX
-- ============================================================================

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid REFERENCES events(id) ON DELETE RESTRICT,
    partner_id uuid REFERENCES partners(id) ON DELETE RESTRICT,
    actor_kind text NOT NULL,
    actor_user_id uuid REFERENCES app_users(id) ON DELETE RESTRICT,
    actor_partner_id uuid REFERENCES partners(id) ON DELETE RESTRICT,
    actor_buyer_session_id uuid
        REFERENCES buyer_selection_sessions(id) ON DELETE RESTRICT,
    system_actor text,
    operation text NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid,
    reservation_id uuid REFERENCES reservations(id) ON DELETE RESTRICT,
    sale_id uuid REFERENCES sales(id) ON DELETE RESTRICT,
    ticket_entitlement_id uuid
        REFERENCES ticket_entitlements(id) ON DELETE RESTRICT,
    previous_state jsonb,
    new_state jsonb,
    reason text,
    idempotency_key_hash bytea,
    correlation_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT audit_events_actor_kind_ck
        CHECK (actor_kind IN ('USER','PARTNER','BUYER_SESSION','SYSTEM')),

    CONSTRAINT audit_events_actor_arc_ck CHECK (
        (
            actor_kind = 'USER'
            AND actor_user_id IS NOT NULL
            AND actor_partner_id IS NULL
            AND actor_buyer_session_id IS NULL
            AND system_actor IS NULL
        )
        OR
        (
            actor_kind = 'PARTNER'
            AND actor_user_id IS NULL
            AND actor_partner_id IS NOT NULL
            AND actor_buyer_session_id IS NULL
            AND system_actor IS NULL
        )
        OR
        (
            actor_kind = 'BUYER_SESSION'
            AND actor_user_id IS NULL
            AND actor_partner_id IS NULL
            AND actor_buyer_session_id IS NOT NULL
            AND system_actor IS NULL
        )
        OR
        (
            actor_kind = 'SYSTEM'
            AND actor_user_id IS NULL
            AND actor_partner_id IS NULL
            AND actor_buyer_session_id IS NULL
            AND system_actor IS NOT NULL
        )
    )
);

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    enqueue_sequence bigint GENERATED ALWAYS AS IDENTITY,
    fact_id uuid NOT NULL,
    event_id uuid REFERENCES events(id) ON DELETE RESTRICT,
    fact_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    last_error text,

    CONSTRAINT outbox_events_enqueue_sequence_uq UNIQUE (enqueue_sequence),
    CONSTRAINT outbox_events_fact_id_uq UNIQUE (fact_id),
    CONSTRAINT outbox_events_attempt_count_ck CHECK (attempt_count >= 0)
);

-- ============================================================================
-- PARTNER WEBHOOKS
-- ============================================================================

CREATE TABLE partner_webhook_endpoints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES partners(id) ON DELETE RESTRICT,
    url text NOT NULL,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,

    CONSTRAINT partner_webhook_endpoints_state_ck
        CHECK (state IN ('ACTIVE','DISABLED')),
    CONSTRAINT partner_webhook_endpoints_id_partner_uq
        UNIQUE (id, partner_id)
);

CREATE TABLE partner_webhook_signing_secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_endpoint_id uuid NOT NULL
        REFERENCES partner_webhook_endpoints(id) ON DELETE RESTRICT,
    secret_ciphertext bytea NOT NULL,
    encryption_key_version integer NOT NULL,
    state text NOT NULL,
    activated_at timestamptz NOT NULL,
    valid_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT webhook_signing_secret_key_version_ck
        CHECK (encryption_key_version > 0),
    CONSTRAINT webhook_signing_secret_state_ck
        CHECK (state IN ('ACTIVE','RETIRING','REVOKED'))
);

CREATE TABLE partner_webhook_subscriptions (
    webhook_endpoint_id uuid NOT NULL
        REFERENCES partner_webhook_endpoints(id) ON DELETE RESTRICT,
    event_type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (webhook_endpoint_id, event_type)
);

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_endpoint_id uuid NOT NULL
        REFERENCES partner_webhook_endpoints(id) ON DELETE RESTRICT,
    outbox_event_id uuid NOT NULL
        REFERENCES outbox_events(id) ON DELETE RESTRICT,
    state text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    last_status_code integer,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    dead_lettered_at timestamptz,

    CONSTRAINT webhook_deliveries_endpoint_event_uq
        UNIQUE (webhook_endpoint_id, outbox_event_id),
    CONSTRAINT webhook_deliveries_attempt_ck
        CHECK (attempt_count >= 0),
    CONSTRAINT webhook_deliveries_state_ck
        CHECK (state IN ('PENDING','DELIVERED','DEAD_LETTER','CANCELLED'))
);

CREATE TABLE webhook_delivery_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_delivery_id uuid NOT NULL
        REFERENCES webhook_deliveries(id) ON DELETE RESTRICT,
    attempt_number integer NOT NULL,
    attempted_at timestamptz NOT NULL,
    duration_ms integer,
    status_code integer,
    error_class text,
    response_excerpt text,

    CONSTRAINT webhook_delivery_attempts_number_uq
        UNIQUE (webhook_delivery_id, attempt_number),
    CONSTRAINT webhook_delivery_attempts_number_ck
        CHECK (attempt_number > 0),
    CONSTRAINT webhook_delivery_attempts_duration_ck
        CHECK (duration_ms IS NULL OR duration_ms >= 0)
);

-- ============================================================================
-- INDEXES
-- ============================================================================

CREATE INDEX reserved_inventory_event_section_idx
ON reserved_inventory_units(event_id, event_section_id, id);

CREATE UNIQUE INDEX reserved_inventory_one_active_claim_uq
ON reserved_inventory_claims(reserved_inventory_unit_id)
WHERE ended_at IS NULL;

CREATE INDEX reserved_claim_reservation_item_idx
ON reserved_inventory_claims(reservation_item_id)
WHERE reservation_item_id IS NOT NULL;

CREATE INDEX reserved_claim_sale_item_idx
ON reserved_inventory_claims(sale_item_id)
WHERE sale_item_id IS NOT NULL;

CREATE INDEX ga_inventory_event_section_idx
ON ga_inventory_pools(event_id, event_section_id, id);

CREATE INDEX ga_allocation_buckets_pool_idx
ON ga_allocation_buckets(ga_pool_id, allocation_id);

CREATE INDEX ga_block_buckets_pool_idx
ON ga_block_buckets(ga_pool_id, block_id);

CREATE INDEX reservations_event_state_idx
ON reservations(event_id, state, created_at);

CREATE INDEX reservations_partner_event_state_idx
ON reservations(partner_id, event_id, state, created_at);

CREATE INDEX reservations_buyer_session_idx
ON reservations(buyer_selection_session_id)
WHERE buyer_selection_session_id IS NOT NULL;

CREATE INDEX reservations_hold_due_idx
ON reservations(hold_expires_at, id)
WHERE state = 'HELD';

CREATE INDEX reservations_retry_due_idx
ON reservations(payment_retry_expires_at, id)
WHERE state = 'PAYMENT_RETRY';

CREATE INDEX reservations_reconcile_due_idx
ON reservations(reconciliation_expires_at, id)
WHERE state = 'RECONCILING';

CREATE INDEX reservation_items_active_by_reservation
ON reservation_items(reservation_id, id)
WHERE removed_at IS NULL;

CREATE INDEX reservation_items_reserved_unit_idx
ON reservation_items(reserved_inventory_unit_id)
WHERE reserved_inventory_unit_id IS NOT NULL;

CREATE INDEX reservation_items_ga_pool_idx
ON reservation_items(ga_pool_id)
WHERE ga_pool_id IS NOT NULL;

CREATE UNIQUE INDEX checkout_attempts_one_active_uq
ON checkout_attempts(reservation_id)
WHERE state = 'ACTIVE';

CREATE INDEX checkout_attempts_active_due_idx
ON checkout_attempts(protection_expires_at, reservation_id)
WHERE state = 'ACTIVE';

CREATE INDEX partner_event_access_event_idx
ON partner_event_access(event_id, partner_id, state);

CREATE INDEX restrictions_event_state_idx
ON inventory_restrictions(event_id, kind, state);

CREATE INDEX allocations_partner_idx
ON allocations(partner_id, restriction_id)
WHERE partner_id IS NOT NULL;

CREATE INDEX allocation_reserved_units_unit_idx
ON allocation_reserved_units(reserved_inventory_unit_id, allocation_id);

CREATE UNIQUE INDEX idempotency_scope_operation_uq
ON idempotency_operations (
    scope_kind,
    COALESCE(partner_id, app_user_id, buyer_selection_session_id),
    operation_type,
    idempotency_key
);

CREATE UNIQUE INDEX ticket_one_replacement_child_uq
ON ticket_entitlements(replaces_ticket_entitlement_id)
WHERE replaces_ticket_entitlement_id IS NOT NULL;

CREATE UNIQUE INDEX ticket_one_active_reserved_unit_uq
ON ticket_entitlements(reserved_inventory_unit_id)
WHERE inventory_kind = 'RESERVED'
  AND status = 'ACTIVE';

CREATE INDEX ticket_event_status_idx
ON ticket_entitlements(event_id, status);

CREATE UNIQUE INDEX qr_credentials_one_active_uq
ON qr_credentials(ticket_entitlement_id)
WHERE status = 'ACTIVE';

CREATE INDEX scan_attempts_event_time_idx
ON scan_attempts(event_id, occurred_at DESC);

CREATE UNIQUE INDEX admissions_one_active_per_ticket_uq
ON admissions(ticket_entitlement_id)
WHERE status = 'ACTIVE';

CREATE INDEX audit_events_event_time_idx
ON audit_events(event_id, occurred_at DESC);

CREATE INDEX audit_events_entity_idx
ON audit_events(entity_type, entity_id, occurred_at DESC);

CREATE INDEX audit_events_reservation_idx
ON audit_events(reservation_id, occurred_at DESC);

CREATE INDEX audit_events_ticket_idx
ON audit_events(ticket_entitlement_id, occurred_at DESC);

CREATE INDEX outbox_pending_idx
ON outbox_events(next_attempt_at, enqueue_sequence)
WHERE processed_at IS NULL;

CREATE UNIQUE INDEX webhook_one_active_secret_uq
ON partner_webhook_signing_secrets(webhook_endpoint_id)
WHERE state = 'ACTIVE';

CREATE INDEX webhook_deliveries_pending_idx
ON webhook_deliveries(next_attempt_at, id)
WHERE state = 'PENDING';

-- ============================================================================
-- HELPER FUNCTIONS
-- ============================================================================

CREATE OR REPLACE FUNCTION tktsync_event_has_protected_history(p_event_id uuid)
RETURNS boolean
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_state text;
BEGIN
    SELECT state
    INTO v_state
    FROM events
    WHERE id = p_event_id;

    IF NOT FOUND THEN
        RETURN false;
    END IF;

    IF v_state <> 'DRAFT' THEN
        RETURN true;
    END IF;

    RETURN
        EXISTS (
            SELECT 1
            FROM inventory_restrictions
            WHERE event_id = p_event_id
        )
        OR EXISTS (
            SELECT 1
            FROM reservations
            WHERE event_id = p_event_id
        )
        OR EXISTS (
            SELECT 1
            FROM sales
            WHERE event_id = p_event_id
        )
        OR EXISTS (
            SELECT 1
            FROM non_public_issuances
            WHERE event_id = p_event_id
        )
        OR EXISTS (
            SELECT 1
            FROM ticket_entitlements
            WHERE event_id = p_event_id
        )
        OR EXISTS (
            SELECT 1
            FROM admissions
            WHERE event_id = p_event_id
        );
END;
$$;

CREATE OR REPLACE FUNCTION tktsync_assert_ga_pool_balance(p_pool_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_capacity integer;
    v_shared_available bigint;
    v_shared_reserved bigint;
    v_shared_sold bigint;
    v_blocked bigint;
    v_alloc_available bigint;
    v_alloc_reserved bigint;
    v_alloc_sold bigint;
    v_alloc_issued bigint;
    v_total bigint;
BEGIN
    SELECT capacity
    INTO v_capacity
    FROM ga_inventory_pools
    WHERE id = p_pool_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT
        available_quantity,
        active_reserved_quantity,
        sold_current_quantity
    INTO
        v_shared_available,
        v_shared_reserved,
        v_shared_sold
    FROM ga_shared_inventory
    WHERE ga_pool_id = p_pool_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'GA pool % has no shared inventory row', p_pool_id
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(blocked_quantity), 0)
    INTO v_blocked
    FROM ga_block_buckets
    WHERE ga_pool_id = p_pool_id;

    SELECT
        COALESCE(SUM(available_quantity), 0),
        COALESCE(SUM(active_reserved_quantity), 0),
        COALESCE(SUM(sold_current_quantity), 0),
        COALESCE(SUM(issued_current_quantity), 0)
    INTO
        v_alloc_available,
        v_alloc_reserved,
        v_alloc_sold,
        v_alloc_issued
    FROM ga_allocation_buckets
    WHERE ga_pool_id = p_pool_id;

    v_total :=
        v_shared_available
        + v_shared_reserved
        + v_shared_sold
        + v_blocked
        + v_alloc_available
        + v_alloc_reserved
        + v_alloc_sold
        + v_alloc_issued;

    IF v_total <> v_capacity THEN
        RAISE EXCEPTION
            'GA pool % imbalance: capacity %, current total %',
            p_pool_id, v_capacity, v_total
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION tktsync_assert_ga_active_reservations(p_pool_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_actual_shared bigint;
    v_expected_shared bigint;
    r record;
    v_expected_bucket bigint;
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM ga_inventory_pools
        WHERE id = p_pool_id
    ) THEN
        RETURN;
    END IF;

    SELECT active_reserved_quantity
    INTO v_actual_shared
    FROM ga_shared_inventory
    WHERE ga_pool_id = p_pool_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'GA pool % has no shared inventory row', p_pool_id
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(ri.quantity), 0)
    INTO v_expected_shared
    FROM reservation_items ri
    JOIN reservations rsv
      ON rsv.id = ri.reservation_id
    WHERE ri.inventory_kind = 'GA'
      AND ri.ga_pool_id = p_pool_id
      AND ri.source_kind = 'SHARED'
      AND ri.removed_at IS NULL
      AND rsv.state IN (
          'HELD','COMMITTING','PAYMENT_RETRY','RECONCILING'
      );

    IF v_actual_shared <> v_expected_shared THEN
        RAISE EXCEPTION
            'GA shared active-reservation mismatch for pool %: bucket %, items %',
            p_pool_id, v_actual_shared, v_expected_shared
            USING ERRCODE = '23514';
    END IF;

    FOR r IN
        SELECT id, active_reserved_quantity
        FROM ga_allocation_buckets
        WHERE ga_pool_id = p_pool_id
    LOOP
        SELECT COALESCE(SUM(ri.quantity), 0)
        INTO v_expected_bucket
        FROM reservation_items ri
        JOIN reservations rsv
          ON rsv.id = ri.reservation_id
        WHERE ri.inventory_kind = 'GA'
          AND ri.source_kind = 'ALLOCATION'
          AND ri.source_ga_allocation_bucket_id = r.id
          AND ri.removed_at IS NULL
          AND rsv.state IN (
              'HELD','COMMITTING','PAYMENT_RETRY','RECONCILING'
          );

        IF r.active_reserved_quantity <> v_expected_bucket THEN
            RAISE EXCEPTION
                'GA allocation active-reservation mismatch for bucket %: bucket %, items %',
                r.id, r.active_reserved_quantity, v_expected_bucket
                USING ERRCODE = '23514';
        END IF;
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION tktsync_assert_qr_ticket_state(p_ticket_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_status text;
    v_active_credentials integer;
BEGIN
    SELECT status
    INTO v_status
    FROM ticket_entitlements
    WHERE id = p_ticket_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*)
    INTO v_active_credentials
    FROM qr_credentials
    WHERE ticket_entitlement_id = p_ticket_id
      AND status = 'ACTIVE';

    IF v_status = 'VOIDED' AND v_active_credentials > 0 THEN
        RAISE EXCEPTION
            'VOIDED ticket % has an ACTIVE QR credential', p_ticket_id
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM qr_credentials q
        JOIN ticket_entitlements t
          ON t.id = q.ticket_entitlement_id
        WHERE q.ticket_entitlement_id = p_ticket_id
          AND q.status = 'ACTIVE'
          AND t.status <> 'ACTIVE'
    ) THEN
        RAISE EXCEPTION
            'ACTIVE QR credential belongs to non-ACTIVE ticket %', p_ticket_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION tktsync_assert_sale_ticket_cardinality(p_sale_item_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_quantity integer;
    v_count integer;
BEGIN
    SELECT quantity
    INTO v_quantity
    FROM sale_items
    WHERE id = p_sale_item_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*)
    INTO v_count
    FROM ticket_entitlements
    WHERE origin_sale_item_id = p_sale_item_id
      AND replaces_ticket_entitlement_id IS NULL;

    IF v_count <> v_quantity THEN
        RAISE EXCEPTION
            'SaleItem % requires % root tickets, found %',
            p_sale_item_id, v_quantity, v_count
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION tktsync_assert_issuance_ticket_cardinality(
    p_issuance_item_id uuid
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_quantity integer;
    v_count integer;
BEGIN
    SELECT quantity
    INTO v_quantity
    FROM non_public_issuance_items
    WHERE id = p_issuance_item_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*)
    INTO v_count
    FROM ticket_entitlements
    WHERE origin_issuance_item_id = p_issuance_item_id
      AND replaces_ticket_entitlement_id IS NULL;

    IF v_count <> v_quantity THEN
        RAISE EXCEPTION
            'NonPublicIssuanceItem % requires % root tickets, found %',
            p_issuance_item_id, v_quantity, v_count
            USING ERRCODE = '23514';
    END IF;
END;
$$;

-- ============================================================================
-- CONSTRAINT / PROTECTION TRIGGER FUNCTIONS
-- ============================================================================

CREATE OR REPLACE FUNCTION ct_validate_restriction_subtype_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind text;
BEGIN
    SELECT kind
    INTO v_kind
    FROM inventory_restrictions
    WHERE id = NEW.restriction_id;

    IF TG_TABLE_NAME = 'blocks' AND v_kind <> 'BLOCK' THEN
        RAISE EXCEPTION
            'Block subtype % does not reference BLOCK restriction',
            NEW.restriction_id
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME = 'allocations' AND v_kind <> 'ALLOCATION' THEN
        RAISE EXCEPTION
            'Allocation subtype % does not reference ALLOCATION restriction',
            NEW.restriction_id
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_allocation_release_destination_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_event_id uuid;
    v_destination_event_id uuid;
    v_cycle boolean;
BEGIN
    SELECT event_id
    INTO v_event_id
    FROM inventory_restrictions
    WHERE id = NEW.restriction_id
      AND kind = 'ALLOCATION';

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'Allocation % has invalid parent restriction',
            NEW.restriction_id
            USING ERRCODE = '23514';
    END IF;

    IF NEW.release_destination_kind = 'ALLOCATION' THEN
        SELECT ir.event_id
        INTO v_destination_event_id
        FROM allocations a
        JOIN inventory_restrictions ir
          ON ir.id = a.restriction_id
        WHERE a.restriction_id = NEW.release_destination_allocation_id;

        IF NOT FOUND OR v_destination_event_id <> v_event_id THEN
            RAISE EXCEPTION
                'Allocation % release destination must belong to same Event',
                NEW.restriction_id
                USING ERRCODE = '23514';
        END IF;

        WITH RECURSIVE chain(id) AS (
            SELECT NEW.release_destination_allocation_id

            UNION

            SELECT a.release_destination_allocation_id
            FROM allocations a
            JOIN chain c
              ON a.restriction_id = c.id
            WHERE a.release_destination_allocation_id IS NOT NULL
        )
        SELECT EXISTS (
            SELECT 1
            FROM chain
            WHERE id = NEW.restriction_id
        )
        INTO v_cycle;

        IF v_cycle THEN
            RAISE EXCEPTION
                'Allocation release-destination cycle detected for %',
                NEW.restriction_id
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.mode = 'CHANNEL'
       AND (
           TG_OP = 'INSERT'
           OR OLD.partner_id IS DISTINCT FROM NEW.partner_id
           OR OLD.mode IS DISTINCT FROM NEW.mode
       )
    THEN
        IF NOT EXISTS (
            SELECT 1
            FROM partner_event_access pea
            WHERE pea.partner_id = NEW.partner_id
              AND pea.event_id = v_event_id
              AND pea.state = 'ACTIVE'
        ) THEN
            RAISE EXCEPTION
                'Channel Allocation Partner lacks ACTIVE Event access'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_ga_pool_balance_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_old_pool uuid;
    v_new_pool uuid;
BEGIN
    IF TG_TABLE_NAME = 'ga_inventory_pools' THEN
        IF TG_OP IN ('UPDATE','DELETE') THEN
            v_old_pool := OLD.id;
        END IF;
        IF TG_OP IN ('INSERT','UPDATE') THEN
            v_new_pool := NEW.id;
        END IF;
    ELSE
        IF TG_OP IN ('UPDATE','DELETE') THEN
            v_old_pool := OLD.ga_pool_id;
        END IF;
        IF TG_OP IN ('INSERT','UPDATE') THEN
            v_new_pool := NEW.ga_pool_id;
        END IF;
    END IF;

    IF v_old_pool IS NOT NULL THEN
        PERFORM tktsync_assert_ga_pool_balance(v_old_pool);
    END IF;

    IF v_new_pool IS NOT NULL
       AND v_new_pool IS DISTINCT FROM v_old_pool
    THEN
        PERFORM tktsync_assert_ga_pool_balance(v_new_pool);
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_ga_active_reservations_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_pool uuid;
BEGIN
    IF TG_TABLE_NAME = 'reservations' THEN
        FOR v_pool IN
            SELECT DISTINCT ri.ga_pool_id
            FROM reservation_items ri
            WHERE ri.reservation_id =
                CASE
                    WHEN TG_OP = 'DELETE' THEN OLD.id
                    ELSE NEW.id
                END
              AND ri.ga_pool_id IS NOT NULL
        LOOP
            PERFORM tktsync_assert_ga_active_reservations(v_pool);
        END LOOP;

        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_TABLE_NAME = 'reservation_items' THEN
        IF TG_OP IN ('UPDATE','DELETE')
           AND OLD.ga_pool_id IS NOT NULL
        THEN
            PERFORM tktsync_assert_ga_active_reservations(OLD.ga_pool_id);
        END IF;

        IF TG_OP IN ('INSERT','UPDATE')
           AND NEW.ga_pool_id IS NOT NULL
           AND (
               TG_OP = 'INSERT'
               OR NEW.ga_pool_id IS DISTINCT FROM OLD.ga_pool_id
           )
        THEN
            PERFORM tktsync_assert_ga_active_reservations(NEW.ga_pool_id);
        END IF;

        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_OP = 'DELETE' THEN
        v_pool := OLD.ga_pool_id;
    ELSE
        v_pool := NEW.ga_pool_id;
    END IF;

    PERFORM tktsync_assert_ga_active_reservations(v_pool);

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_reserved_claim_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_ok boolean;
BEGIN
    IF NEW.ended_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.claim_type = 'RESERVATION' THEN
        SELECT EXISTS (
            SELECT 1
            FROM reservation_items ri
            JOIN reservations r
              ON r.id = ri.reservation_id
            WHERE ri.id = NEW.reservation_item_id
              AND ri.removed_at IS NULL
              AND ri.inventory_kind = 'RESERVED'
              AND ri.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
              AND r.state IN (
                    'HELD','COMMITTING','PAYMENT_RETRY','RECONCILING'
              )
        )
        INTO v_ok;

    ELSIF NEW.claim_type = 'BLOCK' THEN
        SELECT EXISTS (
            SELECT 1
            FROM block_reserved_units bru
            JOIN blocks b
              ON b.restriction_id = bru.block_id
            JOIN inventory_restrictions ir
              ON ir.id = b.restriction_id
            WHERE bru.id = NEW.block_reserved_unit_id
              AND bru.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
              AND bru.released_at IS NULL
              AND ir.kind = 'BLOCK'
              AND ir.state = 'ACTIVE'
        )
        INTO v_ok;

    ELSIF NEW.claim_type = 'ALLOCATION' THEN
        SELECT EXISTS (
            SELECT 1
            FROM allocation_reserved_units aru
            JOIN allocations a
              ON a.restriction_id = aru.allocation_id
            JOIN inventory_restrictions ir
              ON ir.id = a.restriction_id
            WHERE aru.id = NEW.allocation_reserved_unit_id
              AND aru.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
              AND aru.released_at IS NULL
              AND ir.kind = 'ALLOCATION'
              AND ir.state = 'ACTIVE'
        )
        INTO v_ok;

    ELSIF NEW.claim_type = 'SALE' THEN
        SELECT EXISTS (
            SELECT 1
            FROM sale_items si
            JOIN sales s ON s.id = si.sale_id
            WHERE si.id = NEW.sale_item_id
              AND si.inventory_kind = 'RESERVED'
              AND si.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
        )
        INTO v_ok;

    ELSIF NEW.claim_type = 'ISSUANCE' THEN
        SELECT EXISTS (
            SELECT 1
            FROM non_public_issuance_items nii
            JOIN non_public_issuances ni
              ON ni.id = nii.issuance_id
            WHERE nii.id = NEW.issuance_item_id
              AND nii.inventory_kind = 'RESERVED'
              AND nii.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
        )
        INTO v_ok;
    ELSE
        v_ok := false;
    END IF;

    IF NOT COALESCE(v_ok, false) THEN
        RAISE EXCEPTION
            'Invalid active ReservedInventoryClaim % for unit %',
            NEW.claim_type, NEW.reserved_inventory_unit_id
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_enforce_transaction_currency_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_currency char(3);
    v_event_id uuid;
    v_tier_currency char(3);
BEGIN
    IF TG_TABLE_NAME = 'reservation_items' THEN
        SELECT currency, event_id
        INTO v_currency, v_event_id
        FROM reservations
        WHERE id = NEW.reservation_id;

        IF NOT FOUND
           OR v_event_id <> NEW.event_id
           OR v_currency <> NEW.currency
        THEN
            RAISE EXCEPTION
                'ReservationItem transaction currency/Event mismatch'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.price_tier_id IS NOT NULL THEN
            SELECT currency
            INTO v_tier_currency
            FROM event_price_tiers
            WHERE id = NEW.price_tier_id
              AND event_id = NEW.event_id;

            IF NOT FOUND OR v_tier_currency <> NEW.currency THEN
                RAISE EXCEPTION
                    'ReservationItem price tier currency mismatch'
                    USING ERRCODE = '23514';
            END IF;
        END IF;

    ELSIF TG_TABLE_NAME = 'sales' THEN
        SELECT currency
        INTO v_currency
        FROM reservations
        WHERE id = NEW.reservation_id
          AND event_id = NEW.event_id
          AND partner_id = NEW.partner_id;

        IF NOT FOUND OR v_currency <> NEW.currency THEN
            RAISE EXCEPTION
                'Sale currency does not match Reservation'
                USING ERRCODE = '23514';
        END IF;

    ELSIF TG_TABLE_NAME = 'sale_items' THEN
        SELECT currency
        INTO v_currency
        FROM sales
        WHERE id = NEW.sale_id
          AND event_id = NEW.event_id;

        IF NOT FOUND OR v_currency <> NEW.currency THEN
            RAISE EXCEPTION
                'SaleItem currency does not match Sale'
                USING ERRCODE = '23514';
        END IF;

        IF NOT EXISTS (
            SELECT 1
            FROM reservation_items ri
            WHERE ri.id = NEW.reservation_item_id
              AND ri.currency = NEW.currency
        ) THEN
            RAISE EXCEPTION
                'SaleItem currency does not match ReservationItem'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_sale_item_snapshot_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_source_kind text;
    v_source_allocation uuid;
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM sale_items si
        JOIN sales s
          ON s.id = si.sale_id
        JOIN reservations r
          ON r.id = s.reservation_id
        JOIN reservation_items ri
          ON ri.id = si.reservation_item_id
        WHERE si.id = NEW.id
          AND r.state = 'CONFIRMED'
          AND ri.removed_at IS NULL
          AND ri.reservation_id = r.id
          AND ri.event_id = NEW.event_id
          AND s.event_id = NEW.event_id
          AND ri.inventory_kind = NEW.inventory_kind
          AND ri.reserved_inventory_unit_id
                IS NOT DISTINCT FROM NEW.reserved_inventory_unit_id
          AND ri.ga_pool_id
                IS NOT DISTINCT FROM NEW.ga_pool_id
          AND ri.quantity = NEW.quantity
          AND ri.unit_amount_minor = NEW.unit_amount_minor
          AND ri.currency = NEW.currency
    ) THEN
        RAISE EXCEPTION
            'SaleItem does not match confirmed ReservationItem snapshot'
            USING ERRCODE = '23514';
    END IF;

    SELECT source_kind
    INTO v_source_kind
    FROM reservation_items
    WHERE id = NEW.reservation_item_id;

    IF v_source_kind = 'SHARED' THEN
        IF NEW.source_allocation_id IS NOT NULL THEN
            RAISE EXCEPTION
                'Shared ReservationItem cannot create allocation-sourced SaleItem'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        IF NEW.inventory_kind = 'RESERVED' THEN
            SELECT aru.allocation_id
            INTO v_source_allocation
            FROM reservation_items ri
            JOIN allocation_reserved_units aru
              ON aru.id = ri.source_allocation_reserved_unit_id
            WHERE ri.id = NEW.reservation_item_id;
        ELSE
            SELECT gab.allocation_id
            INTO v_source_allocation
            FROM reservation_items ri
            JOIN ga_allocation_buckets gab
              ON gab.id = ri.source_ga_allocation_bucket_id
            WHERE ri.id = NEW.reservation_item_id;
        END IF;

        IF v_source_allocation IS NULL
           OR NEW.source_allocation_id IS DISTINCT FROM v_source_allocation
        THEN
            RAISE EXCEPTION
                'SaleItem Allocation source mismatch'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_non_public_issuance_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM allocations a
        JOIN inventory_restrictions ir
          ON ir.id = a.restriction_id
        JOIN events e
          ON e.id = ir.event_id
        JOIN event_layout_snapshots els
          ON els.event_id = e.id
        WHERE a.restriction_id = NEW.allocation_id
          AND a.mode = 'NON_PUBLIC'
          AND ir.kind = 'ALLOCATION'
          AND ir.event_id = NEW.event_id
          AND e.state NOT IN ('CANCELLED','COMPLETED')
          AND els.finalized_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'NonPublicIssuance requires finalized eligible NON_PUBLIC Allocation'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_issuance_item_source_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_allocation_id uuid;
    v_event_id uuid;
BEGIN
    SELECT allocation_id, event_id
    INTO v_allocation_id, v_event_id
    FROM non_public_issuances
    WHERE id = NEW.issuance_id;

    IF NOT FOUND OR v_event_id <> NEW.event_id THEN
        RAISE EXCEPTION
            'NonPublicIssuanceItem Event mismatch'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.inventory_kind = 'RESERVED' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM allocation_reserved_units aru
            JOIN reserved_inventory_units riu
              ON riu.id = aru.reserved_inventory_unit_id
            WHERE aru.id = NEW.allocation_reserved_unit_id
              AND aru.allocation_id = v_allocation_id
              AND aru.reserved_inventory_unit_id =
                    NEW.reserved_inventory_unit_id
              AND aru.released_at IS NULL
              AND riu.event_id = NEW.event_id
        ) THEN
            RAISE EXCEPTION
                'Reserved issuance item does not match issuance Allocation'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        IF NOT EXISTS (
            SELECT 1
            FROM ga_allocation_buckets gab
            JOIN ga_inventory_pools gp
              ON gp.id = gab.ga_pool_id
            WHERE gab.id = NEW.ga_allocation_bucket_id
              AND gab.allocation_id = v_allocation_id
              AND gab.ga_pool_id = NEW.ga_pool_id
              AND gp.event_id = NEW.event_id
        ) THEN
            RAISE EXCEPTION
                'GA issuance item does not match issuance Allocation'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_ticket_origin_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_origin_event uuid;
    v_kind text;
    v_reserved uuid;
    v_ga uuid;
    v_old_ticket ticket_entitlements%ROWTYPE;
    v_cycle boolean;
BEGIN
    IF NEW.origin_sale_item_id IS NOT NULL THEN
        SELECT
            event_id,
            inventory_kind,
            reserved_inventory_unit_id,
            ga_pool_id
        INTO
            v_origin_event,
            v_kind,
            v_reserved,
            v_ga
        FROM sale_items
        WHERE id = NEW.origin_sale_item_id;
    ELSE
        SELECT
            event_id,
            inventory_kind,
            reserved_inventory_unit_id,
            ga_pool_id
        INTO
            v_origin_event,
            v_kind,
            v_reserved,
            v_ga
        FROM non_public_issuance_items
        WHERE id = NEW.origin_issuance_item_id;
    END IF;

    IF v_origin_event IS NULL
       OR v_origin_event <> NEW.event_id
       OR v_kind <> NEW.inventory_kind
       OR v_reserved IS DISTINCT FROM NEW.reserved_inventory_unit_id
       OR v_ga IS DISTINCT FROM NEW.ga_pool_id
    THEN
        RAISE EXCEPTION
            'TicketEntitlement does not match immutable origin inventory'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.replaces_ticket_entitlement_id IS NOT NULL THEN
        SELECT *
        INTO v_old_ticket
        FROM ticket_entitlements
        WHERE id = NEW.replaces_ticket_entitlement_id;

        IF NOT FOUND
           OR v_old_ticket.status <> 'VOIDED'
           OR v_old_ticket.event_id <> NEW.event_id
           OR v_old_ticket.inventory_kind <> NEW.inventory_kind
           OR v_old_ticket.reserved_inventory_unit_id
                IS DISTINCT FROM NEW.reserved_inventory_unit_id
           OR v_old_ticket.ga_pool_id
                IS DISTINCT FROM NEW.ga_pool_id
           OR v_old_ticket.origin_sale_item_id
                IS DISTINCT FROM NEW.origin_sale_item_id
           OR v_old_ticket.origin_issuance_item_id
                IS DISTINCT FROM NEW.origin_issuance_item_id
        THEN
            RAISE EXCEPTION
                'Replacement Ticket lineage/state mismatch'
                USING ERRCODE = '23514';
        END IF;

        WITH RECURSIVE chain(id) AS (
            SELECT NEW.replaces_ticket_entitlement_id

            UNION

            SELECT t.replaces_ticket_entitlement_id
            FROM ticket_entitlements t
            JOIN chain c
              ON t.id = c.id
            WHERE t.replaces_ticket_entitlement_id IS NOT NULL
        )
        SELECT EXISTS (
            SELECT 1 FROM chain WHERE id = NEW.id
        )
        INTO v_cycle;

        IF v_cycle THEN
            RAISE EXCEPTION
                'Ticket replacement cycle detected'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_origin_ticket_cardinality_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_TABLE_NAME = 'sale_items' THEN
        PERFORM tktsync_assert_sale_ticket_cardinality(NEW.id);

    ELSIF TG_TABLE_NAME = 'non_public_issuance_items' THEN
        PERFORM tktsync_assert_issuance_ticket_cardinality(NEW.id);

    ELSIF TG_TABLE_NAME = 'ticket_entitlements' THEN
        IF NEW.origin_sale_item_id IS NOT NULL THEN
            PERFORM tktsync_assert_sale_ticket_cardinality(
                NEW.origin_sale_item_id
            );
        ELSE
            PERFORM tktsync_assert_issuance_ticket_cardinality(
                NEW.origin_issuance_item_id
            );
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_qr_ticket_state_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_ticket_id uuid;
BEGIN
    IF TG_TABLE_NAME = 'qr_credentials' THEN
        v_ticket_id := NEW.ticket_entitlement_id;
    ELSE
        v_ticket_id := NEW.id;
    END IF;

    PERFORM tktsync_assert_qr_ticket_state(v_ticket_id);

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_layout_component_scope_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_section uuid;
BEGIN
    SELECT id
    INTO v_section
    FROM venue_layout_sections
    WHERE id = NEW.section_id
      AND layout_version_id = NEW.layout_version_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'Layout component Section scope mismatch'
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME = 'venue_layout_seats' THEN
        IF NEW.row_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1
               FROM venue_layout_rows
               WHERE id = NEW.row_id
                 AND layout_version_id = NEW.layout_version_id
                 AND section_id = NEW.section_id
           )
        THEN
            RAISE EXCEPTION
                'Seat Row/Section scope mismatch'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.table_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1
               FROM venue_layout_tables
               WHERE id = NEW.table_id
                 AND layout_version_id = NEW.layout_version_id
                 AND section_id = NEW.section_id
           )
        THEN
            RAISE EXCEPTION
                'Seat Table/Section scope mismatch'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_validate_event_snapshot_materialization_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_source_layout uuid;
    v_source_section uuid;
BEGIN
    IF TG_TABLE_NAME = 'event_layout_snapshots' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM events e
            JOIN venue_layout_versions vlv
              ON vlv.id = NEW.source_layout_version_id
            WHERE e.id = NEW.event_id
              AND e.venue_id = vlv.venue_id
        ) THEN
            RAISE EXCEPTION
                'Event snapshot source layout does not belong to Event Venue'
                USING ERRCODE = '23514';
        END IF;

        RETURN NEW;
    END IF;

    SELECT source_layout_version_id
    INTO v_source_layout
    FROM event_layout_snapshots
    WHERE event_id = NEW.event_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'Event inventory materialization requires EventLayoutSnapshot'
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME = 'event_sections' THEN
        IF NEW.source_layout_section_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1
               FROM venue_layout_sections
               WHERE id = NEW.source_layout_section_id
                 AND layout_version_id = v_source_layout
           )
        THEN
            RAISE EXCEPTION
                'EventSection source is outside Event snapshot layout'
                USING ERRCODE = '23514';
        END IF;

    ELSIF TG_TABLE_NAME = 'reserved_inventory_units' THEN
        IF NEW.source_venue_seat_id IS NOT NULL THEN
            SELECT source_layout_section_id
            INTO v_source_section
            FROM event_sections
            WHERE id = NEW.event_section_id
              AND event_id = NEW.event_id;

            IF v_source_section IS NULL
               OR NOT EXISTS (
                   SELECT 1
                   FROM venue_layout_seats
                   WHERE id = NEW.source_venue_seat_id
                     AND layout_version_id = v_source_layout
                     AND section_id = v_source_section
               )
            THEN
                RAISE EXCEPTION
                    'ReservedInventoryUnit source seat/snapshot scope mismatch'
                    USING ERRCODE = '23514';
            END IF;
        END IF;

    ELSIF TG_TABLE_NAME = 'ga_inventory_pools' THEN
        IF NEW.source_ga_zone_id IS NOT NULL THEN
            SELECT source_layout_section_id
            INTO v_source_section
            FROM event_sections
            WHERE id = NEW.event_section_id
              AND event_id = NEW.event_id;

            IF v_source_section IS NULL
               OR NOT EXISTS (
                   SELECT 1
                   FROM venue_layout_ga_zones
                   WHERE id = NEW.source_ga_zone_id
                     AND layout_version_id = v_source_layout
                     AND section_id = v_source_section
               )
            THEN
                RAISE EXCEPTION
                    'GA pool source zone/snapshot scope mismatch'
                    USING ERRCODE = '23514';
            END IF;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_protect_published_layout_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_state text;
    v_layout uuid;
BEGIN
    IF TG_TABLE_NAME = 'venue_layout_versions' THEN
        IF TG_OP = 'DELETE' THEN
            IF OLD.state IN ('PUBLISHED','RETIRED') THEN
                RAISE EXCEPTION
                    'Published/retired layout versions cannot be deleted'
                    USING ERRCODE = '23514';
            END IF;
            RETURN OLD;
        END IF;

        IF OLD.state IN ('PUBLISHED','RETIRED') THEN
            IF ROW(
                NEW.id,
                NEW.venue_id,
                NEW.version_number,
                NEW.geometry_json,
                NEW.content_hash,
                NEW.published_at
            ) IS DISTINCT FROM ROW(
                OLD.id,
                OLD.venue_id,
                OLD.version_number,
                OLD.geometry_json,
                OLD.content_hash,
                OLD.published_at
            ) THEN
                RAISE EXCEPTION
                    'Published layout physical identity is immutable'
                    USING ERRCODE = '23514';
            END IF;

            IF NOT (
                NEW.state = OLD.state
                OR (
                    OLD.state = 'PUBLISHED'
                    AND NEW.state = 'RETIRED'
                )
            ) THEN
                RAISE EXCEPTION
                    'Invalid published layout lifecycle rewrite'
                    USING ERRCODE = '23514';
            END IF;
        END IF;

        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        v_layout := OLD.layout_version_id;
    ELSE
        v_layout := NEW.layout_version_id;
    END IF;

    SELECT state
    INTO v_state
    FROM venue_layout_versions
    WHERE id = v_layout;

    IF v_state IN ('PUBLISHED','RETIRED') THEN
        RAISE EXCEPTION
            'Published layout components are immutable'
            USING ERRCODE = '23514';
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE OR REPLACE FUNCTION ct_protect_live_event_inventory_identity_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_event_id uuid;
BEGIN
    IF TG_TABLE_NAME = 'event_layout_snapshots' THEN
        v_event_id :=
            CASE WHEN TG_OP = 'DELETE' THEN OLD.event_id ELSE NEW.event_id END;

        IF tktsync_event_has_protected_history(v_event_id) THEN
            IF TG_OP IN ('INSERT','DELETE') THEN
                RAISE EXCEPTION
                    'Protected Event snapshot identity cannot be inserted/deleted'
                    USING ERRCODE = '23514';
            END IF;

            IF ROW(
                NEW.source_layout_version_id,
                NEW.snapshot_json,
                NEW.content_hash,
                NEW.finalized_at
            ) IS DISTINCT FROM ROW(
                OLD.source_layout_version_id,
                OLD.snapshot_json,
                OLD.content_hash,
                OLD.finalized_at
            ) THEN
                RAISE EXCEPTION
                    'Protected Event snapshot physical identity is immutable'
                    USING ERRCODE = '23514';
            END IF;
        END IF;

        RETURN COALESCE(NEW, OLD);
    END IF;

    v_event_id :=
        CASE WHEN TG_OP = 'DELETE' THEN OLD.event_id ELSE NEW.event_id END;

    IF NOT tktsync_event_has_protected_history(v_event_id) THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_OP IN ('INSERT','DELETE') THEN
        RAISE EXCEPTION
            'Protected Event inventory physical identity cannot be added/deleted'
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME = 'event_sections' THEN
        IF ROW(
            NEW.event_id,
            NEW.source_layout_section_id,
            NEW.snapshot_object_key
        ) IS DISTINCT FROM ROW(
            OLD.event_id,
            OLD.source_layout_section_id,
            OLD.snapshot_object_key
        ) THEN
            RAISE EXCEPTION
                'Protected EventSection identity is immutable'
                USING ERRCODE = '23514';
        END IF;

    ELSIF TG_TABLE_NAME = 'reserved_inventory_units' THEN
        IF ROW(
            NEW.event_id,
            NEW.event_section_id,
            NEW.source_venue_seat_id,
            NEW.snapshot_object_key
        ) IS DISTINCT FROM ROW(
            OLD.event_id,
            OLD.event_section_id,
            OLD.source_venue_seat_id,
            OLD.snapshot_object_key
        ) THEN
            RAISE EXCEPTION
                'Protected ReservedInventoryUnit identity is immutable'
                USING ERRCODE = '23514';
        END IF;

    ELSIF TG_TABLE_NAME = 'ga_inventory_pools' THEN
        IF ROW(
            NEW.event_id,
            NEW.event_section_id,
            NEW.source_ga_zone_id,
            NEW.snapshot_object_key
        ) IS DISTINCT FROM ROW(
            OLD.event_id,
            OLD.event_section_id,
            OLD.source_ga_zone_id,
            OLD.snapshot_object_key
        ) THEN
            RAISE EXCEPTION
                'Protected GA pool identity is immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_protect_reservation_item_snapshot_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_state text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION
            'ReservationItems are append-preserved; use removed_at while HELD'
            USING ERRCODE = '23514';
    END IF;

    IF ROW(
        NEW.id,
        NEW.reservation_id,
        NEW.event_id,
        NEW.inventory_kind,
        NEW.reserved_inventory_unit_id,
        NEW.ga_pool_id,
        NEW.quantity,
        NEW.source_kind,
        NEW.source_allocation_reserved_unit_id,
        NEW.source_ga_allocation_bucket_id,
        NEW.price_tier_id,
        NEW.unit_amount_minor,
        NEW.currency,
        NEW.price_tier_label_snapshot,
        NEW.commercial_terms,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.id,
        OLD.reservation_id,
        OLD.event_id,
        OLD.inventory_kind,
        OLD.reserved_inventory_unit_id,
        OLD.ga_pool_id,
        OLD.quantity,
        OLD.source_kind,
        OLD.source_allocation_reserved_unit_id,
        OLD.source_ga_allocation_bucket_id,
        OLD.price_tier_id,
        OLD.unit_amount_minor,
        OLD.currency,
        OLD.price_tier_label_snapshot,
        OLD.commercial_terms,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION
            'ReservationItem identity/source/price snapshot is immutable'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.removed_at IS DISTINCT FROM OLD.removed_at THEN
        SELECT state
        INTO v_state
        FROM reservations
        WHERE id = OLD.reservation_id;

        IF v_state <> 'HELD'
           OR OLD.removed_at IS NOT NULL
           OR NEW.removed_at IS NULL
        THEN
            RAISE EXCEPTION
                'ReservationItem may only be removed once while Reservation is HELD'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_protect_ticket_identity_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION
            'TicketEntitlement history cannot be deleted'
            USING ERRCODE = '23514';
    END IF;

    IF ROW(
        NEW.id,
        NEW.event_id,
        NEW.origin_sale_item_id,
        NEW.origin_issuance_item_id,
        NEW.replaces_ticket_entitlement_id,
        NEW.inventory_kind,
        NEW.reserved_inventory_unit_id,
        NEW.ga_pool_id,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.id,
        OLD.event_id,
        OLD.origin_sale_item_id,
        OLD.origin_issuance_item_id,
        OLD.replaces_ticket_entitlement_id,
        OLD.inventory_kind,
        OLD.reserved_inventory_unit_id,
        OLD.ga_pool_id,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION
            'Ticket entitlement identity/origin is immutable'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_protect_qr_identity_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION
            'QRCredential history cannot be deleted'
            USING ERRCODE = '23514';
    END IF;

    IF ROW(
        NEW.id,
        NEW.ticket_entitlement_id,
        NEW.token_hash,
        NEW.token_key_version,
        NEW.issued_at,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.id,
        OLD.ticket_entitlement_id,
        OLD.token_hash,
        OLD.token_key_version,
        OLD.issued_at,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION
            'QRCredential identity/token material is immutable'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION ct_prevent_immutable_fact_update_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION
            'Immutable historical fact % cannot be deleted',
            TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME IN (
        'sales',
        'sale_items',
        'non_public_issuances',
        'non_public_issuance_items',
        'audit_events',
        'scan_attempts'
    ) THEN
        RAISE EXCEPTION
            'Immutable historical fact % cannot be updated',
            TG_TABLE_NAME
            USING ERRCODE = '23514';
    END IF;

    IF TG_TABLE_NAME = 'reserved_inventory_claims' THEN
        IF ROW(
            NEW.id,
            NEW.reserved_inventory_unit_id,
            NEW.claim_type,
            NEW.reservation_item_id,
            NEW.block_reserved_unit_id,
            NEW.allocation_reserved_unit_id,
            NEW.sale_item_id,
            NEW.issuance_item_id,
            NEW.activated_at
        ) IS DISTINCT FROM ROW(
            OLD.id,
            OLD.reserved_inventory_unit_id,
            OLD.claim_type,
            OLD.reservation_item_id,
            OLD.block_reserved_unit_id,
            OLD.allocation_reserved_unit_id,
            OLD.sale_item_id,
            OLD.issuance_item_id,
            OLD.activated_at
        ) THEN
            RAISE EXCEPTION
                'ReservedInventoryClaim identity/source is immutable'
                USING ERRCODE = '23514';
        END IF;

        IF OLD.ended_at IS NOT NULL
           AND ROW(NEW.ended_at, NEW.end_reason)
               IS DISTINCT FROM ROW(OLD.ended_at, OLD.end_reason)
        THEN
            RAISE EXCEPTION
                'Ended ReservedInventoryClaim history is immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF TG_TABLE_NAME = 'outbox_events' THEN
        IF ROW(
            NEW.id,
            NEW.enqueue_sequence,
            NEW.fact_id,
            NEW.event_id,
            NEW.fact_type,
            NEW.aggregate_type,
            NEW.aggregate_id,
            NEW.payload,
            NEW.created_at
        ) IS DISTINCT FROM ROW(
            OLD.id,
            OLD.enqueue_sequence,
            OLD.fact_id,
            OLD.event_id,
            OLD.fact_type,
            OLD.aggregate_type,
            OLD.aggregate_id,
            OLD.payload,
            OLD.created_at
        ) THEN
            RAISE EXCEPTION
                'Outbox fact identity/payload is immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

-- ============================================================================
-- CONSTRAINT TRIGGERS
-- ============================================================================

CREATE CONSTRAINT TRIGGER ct_validate_block_subtype
AFTER INSERT OR UPDATE ON blocks
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_restriction_subtype_fn();

CREATE CONSTRAINT TRIGGER ct_validate_allocation_subtype
AFTER INSERT OR UPDATE ON allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_restriction_subtype_fn();

CREATE CONSTRAINT TRIGGER ct_validate_allocation_release_destination
AFTER INSERT OR UPDATE ON allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_allocation_release_destination_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_pool_balance_pool
AFTER INSERT OR UPDATE OR DELETE ON ga_inventory_pools
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_pool_balance_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_pool_balance_shared
AFTER INSERT OR UPDATE OR DELETE ON ga_shared_inventory
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_pool_balance_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_pool_balance_block
AFTER INSERT OR UPDATE OR DELETE ON ga_block_buckets
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_pool_balance_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_pool_balance_allocation
AFTER INSERT OR UPDATE OR DELETE ON ga_allocation_buckets
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_pool_balance_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_active_reservations_reservation
AFTER INSERT OR UPDATE ON reservations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_active_reservations_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_active_reservations_item
AFTER INSERT OR UPDATE OR DELETE ON reservation_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_active_reservations_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_active_reservations_shared
AFTER INSERT OR UPDATE OR DELETE ON ga_shared_inventory
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_active_reservations_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ga_active_reservations_allocation
AFTER INSERT OR UPDATE OR DELETE ON ga_allocation_buckets
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ga_active_reservations_fn();

CREATE CONSTRAINT TRIGGER ct_validate_reserved_claim
AFTER INSERT OR UPDATE ON reserved_inventory_claims
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_reserved_claim_fn();

CREATE CONSTRAINT TRIGGER ct_enforce_transaction_currency_reservation_item
AFTER INSERT OR UPDATE ON reservation_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_enforce_transaction_currency_fn();

CREATE CONSTRAINT TRIGGER ct_enforce_transaction_currency_sale
AFTER INSERT OR UPDATE ON sales
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_enforce_transaction_currency_fn();

CREATE CONSTRAINT TRIGGER ct_enforce_transaction_currency_sale_item
AFTER INSERT OR UPDATE ON sale_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_enforce_transaction_currency_fn();

CREATE CONSTRAINT TRIGGER ct_validate_sale_item_snapshot
AFTER INSERT OR UPDATE ON sale_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_sale_item_snapshot_fn();

CREATE CONSTRAINT TRIGGER ct_validate_non_public_issuance
AFTER INSERT OR UPDATE ON non_public_issuances
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_non_public_issuance_fn();

CREATE CONSTRAINT TRIGGER ct_validate_issuance_item_source
AFTER INSERT OR UPDATE ON non_public_issuance_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_issuance_item_source_fn();

CREATE CONSTRAINT TRIGGER ct_validate_ticket_origin
AFTER INSERT OR UPDATE ON ticket_entitlements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_ticket_origin_fn();

CREATE CONSTRAINT TRIGGER ct_validate_origin_ticket_cardinality_sale_item
AFTER INSERT OR UPDATE ON sale_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_origin_ticket_cardinality_fn();

CREATE CONSTRAINT TRIGGER ct_validate_origin_ticket_cardinality_issuance_item
AFTER INSERT OR UPDATE ON non_public_issuance_items
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_origin_ticket_cardinality_fn();

CREATE CONSTRAINT TRIGGER ct_validate_origin_ticket_cardinality_ticket
AFTER INSERT OR UPDATE ON ticket_entitlements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_origin_ticket_cardinality_fn();

CREATE CONSTRAINT TRIGGER ct_validate_qr_ticket_state_qr
AFTER INSERT OR UPDATE ON qr_credentials
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_qr_ticket_state_fn();

CREATE CONSTRAINT TRIGGER ct_validate_qr_ticket_state_ticket
AFTER INSERT OR UPDATE ON ticket_entitlements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_qr_ticket_state_fn();

CREATE CONSTRAINT TRIGGER ct_validate_layout_component_scope_row
AFTER INSERT OR UPDATE ON venue_layout_rows
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_layout_component_scope_fn();

CREATE CONSTRAINT TRIGGER ct_validate_layout_component_scope_table
AFTER INSERT OR UPDATE ON venue_layout_tables
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_layout_component_scope_fn();

CREATE CONSTRAINT TRIGGER ct_validate_layout_component_scope_seat
AFTER INSERT OR UPDATE ON venue_layout_seats
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_layout_component_scope_fn();

CREATE CONSTRAINT TRIGGER ct_validate_layout_component_scope_ga_zone
AFTER INSERT OR UPDATE ON venue_layout_ga_zones
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_layout_component_scope_fn();

CREATE CONSTRAINT TRIGGER ct_validate_event_snapshot_materialization_snapshot
AFTER INSERT OR UPDATE ON event_layout_snapshots
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_event_snapshot_materialization_fn();

CREATE CONSTRAINT TRIGGER ct_validate_event_snapshot_materialization_section
AFTER INSERT OR UPDATE ON event_sections
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_event_snapshot_materialization_fn();

CREATE CONSTRAINT TRIGGER ct_validate_event_snapshot_materialization_reserved
AFTER INSERT OR UPDATE ON reserved_inventory_units
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_event_snapshot_materialization_fn();

CREATE CONSTRAINT TRIGGER ct_validate_event_snapshot_materialization_ga
AFTER INSERT OR UPDATE ON ga_inventory_pools
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ct_validate_event_snapshot_materialization_fn();

-- ============================================================================
-- PROTECTION TRIGGERS
-- ============================================================================

CREATE TRIGGER ct_protect_published_layout_version
BEFORE UPDATE OR DELETE ON venue_layout_versions
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_published_layout_sections
BEFORE INSERT OR UPDATE OR DELETE ON venue_layout_sections
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_published_layout_rows
BEFORE INSERT OR UPDATE OR DELETE ON venue_layout_rows
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_published_layout_tables
BEFORE INSERT OR UPDATE OR DELETE ON venue_layout_tables
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_published_layout_seats
BEFORE INSERT OR UPDATE OR DELETE ON venue_layout_seats
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_published_layout_ga
BEFORE INSERT OR UPDATE OR DELETE ON venue_layout_ga_zones
FOR EACH ROW
EXECUTE FUNCTION ct_protect_published_layout_fn();

CREATE TRIGGER ct_protect_live_event_snapshot
BEFORE INSERT OR UPDATE OR DELETE ON event_layout_snapshots
FOR EACH ROW
EXECUTE FUNCTION ct_protect_live_event_inventory_identity_fn();

CREATE TRIGGER ct_protect_live_event_sections
BEFORE INSERT OR UPDATE OR DELETE ON event_sections
FOR EACH ROW
EXECUTE FUNCTION ct_protect_live_event_inventory_identity_fn();

CREATE TRIGGER ct_protect_live_reserved_inventory
BEFORE INSERT OR UPDATE OR DELETE ON reserved_inventory_units
FOR EACH ROW
EXECUTE FUNCTION ct_protect_live_event_inventory_identity_fn();

CREATE TRIGGER ct_protect_live_ga_inventory
BEFORE INSERT OR UPDATE OR DELETE ON ga_inventory_pools
FOR EACH ROW
EXECUTE FUNCTION ct_protect_live_event_inventory_identity_fn();

CREATE TRIGGER ct_protect_reservation_item_snapshot
BEFORE UPDATE OR DELETE ON reservation_items
FOR EACH ROW
EXECUTE FUNCTION ct_protect_reservation_item_snapshot_fn();

CREATE TRIGGER ct_protect_ticket_identity
BEFORE UPDATE OR DELETE ON ticket_entitlements
FOR EACH ROW
EXECUTE FUNCTION ct_protect_ticket_identity_fn();

CREATE TRIGGER ct_protect_qr_identity
BEFORE UPDATE OR DELETE ON qr_credentials
FOR EACH ROW
EXECUTE FUNCTION ct_protect_qr_identity_fn();

CREATE TRIGGER ct_prevent_immutable_sales
BEFORE UPDATE OR DELETE ON sales
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_prevent_immutable_sale_items
BEFORE UPDATE OR DELETE ON sale_items
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_prevent_immutable_issuances
BEFORE UPDATE OR DELETE ON non_public_issuances
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_prevent_immutable_issuance_items
BEFORE UPDATE OR DELETE ON non_public_issuance_items
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_prevent_immutable_scan_attempts
BEFORE UPDATE OR DELETE ON scan_attempts
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_prevent_immutable_audit
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_protect_claim_history
BEFORE UPDATE OR DELETE ON reserved_inventory_claims
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

CREATE TRIGGER ct_protect_outbox_fact
BEFORE UPDATE OR DELETE ON outbox_events
FOR EACH ROW
EXECUTE FUNCTION ct_prevent_immutable_fact_update_fn();

-- ============================================================================
-- DERIVED VIEWS
-- ============================================================================

CREATE VIEW v_reserved_inventory_current_state AS
SELECT
    riu.id AS reserved_inventory_unit_id,
    riu.event_id,
    riu.event_section_id,
    riu.snapshot_object_key,
    riu.row_label,
    riu.seat_label,
    riu.table_label,
    riu.display_label,
    CASE
        WHEN ric.id IS NULL THEN 'AVAILABLE'
        WHEN ric.claim_type = 'RESERVATION' THEN 'RESERVED'
        WHEN ric.claim_type = 'BLOCK' THEN 'BLOCKED'
        WHEN ric.claim_type = 'ALLOCATION' THEN 'ALLOCATED'
        WHEN ric.claim_type = 'SALE' THEN 'SOLD'
        WHEN ric.claim_type = 'ISSUANCE' THEN 'ISSUED'
    END AS disposition,
    ric.id AS current_claim_id,
    ric.claim_type AS current_claim_type,
    COALESCE(
        riu.price_tier_override_id,
        es.default_price_tier_id
    ) AS effective_price_tier_id,
    ept.name AS price_tier_name,
    ept.amount_minor,
    ept.currency
FROM reserved_inventory_units riu
JOIN event_sections es
  ON es.id = riu.event_section_id
LEFT JOIN reserved_inventory_claims ric
  ON ric.reserved_inventory_unit_id = riu.id
 AND ric.ended_at IS NULL
LEFT JOIN event_price_tiers ept
  ON ept.id = COALESCE(
      riu.price_tier_override_id,
      es.default_price_tier_id
  );

CREATE VIEW v_ga_inventory_current_summary AS
SELECT
    gp.id AS ga_pool_id,
    gp.event_id,
    gp.event_section_id,
    gp.name,
    gp.capacity,
    gp.price_tier_id,
    ept.name AS price_tier_name,
    ept.amount_minor,
    ept.currency,
    gsi.available_quantity AS shared_available_quantity,
    gsi.active_reserved_quantity AS shared_active_reserved_quantity,
    gsi.sold_current_quantity AS shared_sold_current_quantity,
    COALESCE((
        SELECT SUM(gbb.blocked_quantity)
        FROM ga_block_buckets gbb
        WHERE gbb.ga_pool_id = gp.id
    ), 0) AS blocked_current_quantity,
    COALESCE((
        SELECT SUM(gab.available_quantity)
        FROM ga_allocation_buckets gab
        WHERE gab.ga_pool_id = gp.id
    ), 0) AS allocated_available_quantity,
    (
        gsi.active_reserved_quantity
        + COALESCE((
            SELECT SUM(gab.active_reserved_quantity)
            FROM ga_allocation_buckets gab
            WHERE gab.ga_pool_id = gp.id
        ), 0)
    ) AS active_reserved_quantity,
    (
        gsi.sold_current_quantity
        + COALESCE((
            SELECT SUM(gab.sold_current_quantity)
            FROM ga_allocation_buckets gab
            WHERE gab.ga_pool_id = gp.id
        ), 0)
    ) AS sold_current_quantity,
    COALESCE((
        SELECT SUM(gab.issued_current_quantity)
        FROM ga_allocation_buckets gab
        WHERE gab.ga_pool_id = gp.id
    ), 0) AS issued_current_quantity
FROM ga_inventory_pools gp
JOIN ga_shared_inventory gsi
  ON gsi.ga_pool_id = gp.id
LEFT JOIN event_price_tiers ept
  ON ept.id = gp.price_tier_id;

COMMIT;

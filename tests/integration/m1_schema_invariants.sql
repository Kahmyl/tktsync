\set ON_ERROR_STOP on

DO $$
DECLARE
    v_name text;
    v_required text[] := ARRAY[
        'venues',
        'venue_layout_versions',
        'venue_layout_sections',
        'venue_layout_rows',
        'venue_layout_tables',
        'venue_layout_seats',
        'venue_layout_ga_zones',
        'app_users',
        'platform_user_roles',
        'partners',
        'partner_credentials',
        'events',
        'event_transaction_policies',
        'event_layout_snapshots',
        'event_price_tiers',
        'event_sections',
        'reserved_inventory_units',
        'ga_inventory_pools',
        'ga_shared_inventory',
        'event_staff_assignments',
        'partner_event_access',
        'buyer_selection_sessions',
        'inventory_restrictions',
        'blocks',
        'allocations',
        'block_reserved_units',
        'allocation_reserved_units',
        'ga_block_buckets',
        'ga_allocation_buckets',
        'idempotency_operations',
        'reservations',
        'reservation_items',
        'checkout_attempts',
        'sales',
        'sale_items',
        'non_public_issuances',
        'non_public_issuance_items',
        'ticket_entitlements',
        'ticket_attendee_details',
        'qr_credentials',
        'reserved_inventory_claims',
        'scan_attempts',
        'admissions',
        'audit_events',
        'outbox_events',
        'partner_webhook_endpoints',
        'partner_webhook_signing_secrets',
        'partner_webhook_subscriptions',
        'webhook_deliveries',
        'webhook_delivery_attempts'
    ];
BEGIN
    FOREACH v_name IN ARRAY v_required LOOP
        IF to_regclass('public.' || v_name) IS NULL THEN
            RAISE EXCEPTION 'Missing M1 table: %', v_name;
        END IF;
    END LOOP;

    IF to_regclass('public.v_reserved_inventory_current_state') IS NULL THEN
        RAISE EXCEPTION 'Missing reserved inventory current-state view';
    END IF;

    IF to_regclass('public.v_ga_inventory_current_summary') IS NULL THEN
        RAISE EXCEPTION 'Missing GA current-summary view';
    END IF;

    RAISE NOTICE 'M1 object-presence verification passed';
END;
$$;

BEGIN;

SET CONSTRAINTS ALL DEFERRED;

INSERT INTO app_users (
    id,
    auth_provider,
    auth_subject,
    display_name,
    state
) VALUES (
    '00000000-0000-0000-0000-000000000001',
    'test',
    'm1-schema-verifier',
    'M1 Schema Verifier',
    'ACTIVE'
);

INSERT INTO venues (
    id,
    name
) VALUES (
    '00000000-0000-0000-0000-000000000002',
    'M1 Test Venue'
);

INSERT INTO venue_layout_versions (
    id,
    venue_id,
    version_number,
    state,
    geometry_json,
    published_at
) VALUES (
    '00000000-0000-0000-0000-000000000003',
    '00000000-0000-0000-0000-000000000002',
    1,
    'PUBLISHED',
    '{}'::jsonb,
    now()
);

INSERT INTO events (
    id,
    venue_id,
    name,
    state,
    admission_policy
) VALUES (
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000002',
    'M1 Test Event',
    'DRAFT',
    'SINGLE_ENTRY'
);

INSERT INTO event_layout_snapshots (
    event_id,
    source_layout_version_id,
    snapshot_json,
    finalized_at
) VALUES (
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000003',
    '{}'::jsonb,
    now()
);

INSERT INTO event_sections (
    id,
    event_id,
    snapshot_object_key,
    name,
    sort_order
) VALUES (
    '00000000-0000-0000-0000-000000000005',
    '00000000-0000-0000-0000-000000000004',
    'section-test',
    'Test Section',
    1
);

INSERT INTO reserved_inventory_units (
    id,
    event_id,
    event_section_id,
    snapshot_object_key,
    seat_label,
    display_label
) VALUES
(
    '00000000-0000-0000-0000-000000000006',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000005',
    'seat-1',
    '1',
    'Seat 1'
),
(
    '00000000-0000-0000-0000-000000000007',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000005',
    'seat-2',
    '2',
    'Seat 2'
);

INSERT INTO ga_inventory_pools (
    id,
    event_id,
    event_section_id,
    snapshot_object_key,
    name,
    capacity
) VALUES (
    '00000000-0000-0000-0000-000000000008',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000005',
    'ga-test',
    'Test GA',
    10
);

INSERT INTO ga_shared_inventory (
    ga_pool_id,
    available_quantity,
    active_reserved_quantity,
    sold_current_quantity
) VALUES (
    '00000000-0000-0000-0000-000000000008',
    10,
    0,
    0
);

-- Two valid Blocks referencing the same seat membership context.
INSERT INTO inventory_restrictions (
    id,
    event_id,
    kind,
    state,
    purpose,
    created_by_user_id
) VALUES
(
    '00000000-0000-0000-0000-000000000010',
    '00000000-0000-0000-0000-000000000004',
    'BLOCK',
    'ACTIVE',
    'TEST',
    '00000000-0000-0000-0000-000000000001'
),
(
    '00000000-0000-0000-0000-000000000011',
    '00000000-0000-0000-0000-000000000004',
    'BLOCK',
    'ACTIVE',
    'TEST',
    '00000000-0000-0000-0000-000000000001'
);

INSERT INTO blocks(restriction_id) VALUES
('00000000-0000-0000-0000-000000000010'),
('00000000-0000-0000-0000-000000000011');

INSERT INTO block_reserved_units (
    id,
    block_id,
    reserved_inventory_unit_id
) VALUES
(
    '00000000-0000-0000-0000-000000000012',
    '00000000-0000-0000-0000-000000000010',
    '00000000-0000-0000-0000-000000000006'
),
(
    '00000000-0000-0000-0000-000000000013',
    '00000000-0000-0000-0000-000000000011',
    '00000000-0000-0000-0000-000000000006'
);

INSERT INTO reserved_inventory_claims (
    id,
    reserved_inventory_unit_id,
    claim_type,
    block_reserved_unit_id,
    activated_at
) VALUES (
    '00000000-0000-0000-0000-000000000014',
    '00000000-0000-0000-0000-000000000006',
    'BLOCK',
    '00000000-0000-0000-0000-000000000012',
    now()
);

-- Non-public Allocation + issuance fixture for ticket/QR/admission tests.
INSERT INTO inventory_restrictions (
    id,
    event_id,
    kind,
    state,
    purpose,
    created_by_user_id
) VALUES (
    '00000000-0000-0000-0000-000000000020',
    '00000000-0000-0000-0000-000000000004',
    'ALLOCATION',
    'ACTIVE',
    'COMP',
    '00000000-0000-0000-0000-000000000001'
);

INSERT INTO allocations (
    restriction_id,
    mode,
    release_destination_kind
) VALUES (
    '00000000-0000-0000-0000-000000000020',
    'NON_PUBLIC',
    'SHARED'
);

INSERT INTO allocation_reserved_units (
    id,
    allocation_id,
    reserved_inventory_unit_id
) VALUES (
    '00000000-0000-0000-0000-000000000021',
    '00000000-0000-0000-0000-000000000020',
    '00000000-0000-0000-0000-000000000007'
);

INSERT INTO non_public_issuances (
    id,
    event_id,
    allocation_id,
    issued_by_user_id,
    issued_at
) VALUES (
    '00000000-0000-0000-0000-000000000022',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000020',
    '00000000-0000-0000-0000-000000000001',
    now()
);

INSERT INTO non_public_issuance_items (
    id,
    issuance_id,
    event_id,
    inventory_kind,
    reserved_inventory_unit_id,
    allocation_reserved_unit_id,
    quantity
) VALUES (
    '00000000-0000-0000-0000-000000000023',
    '00000000-0000-0000-0000-000000000022',
    '00000000-0000-0000-0000-000000000004',
    'RESERVED',
    '00000000-0000-0000-0000-000000000007',
    '00000000-0000-0000-0000-000000000021',
    1
);

INSERT INTO ticket_entitlements (
    id,
    event_id,
    origin_issuance_item_id,
    inventory_kind,
    reserved_inventory_unit_id,
    status
) VALUES (
    '00000000-0000-0000-0000-000000000024',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000023',
    'RESERVED',
    '00000000-0000-0000-0000-000000000007',
    'ACTIVE'
);

INSERT INTO qr_credentials (
    id,
    ticket_entitlement_id,
    token_hash,
    token_key_version,
    status,
    issued_at
) VALUES (
    '00000000-0000-0000-0000-000000000025',
    '00000000-0000-0000-0000-000000000024',
    decode('0123456789abcdef', 'hex'),
    1,
    'ACTIVE',
    now()
);

INSERT INTO reserved_inventory_claims (
    id,
    reserved_inventory_unit_id,
    claim_type,
    issuance_item_id,
    activated_at
) VALUES (
    '00000000-0000-0000-0000-000000000026',
    '00000000-0000-0000-0000-000000000007',
    'ISSUANCE',
    '00000000-0000-0000-0000-000000000023',
    now()
);

SET CONSTRAINTS ALL IMMEDIATE;
SET CONSTRAINTS ALL DEFERRED;

DO $$
BEGIN
    BEGIN
        INSERT INTO reserved_inventory_claims (
            reserved_inventory_unit_id,
            claim_type,
            block_reserved_unit_id,
            activated_at
        ) VALUES (
            '00000000-0000-0000-0000-000000000006',
            'BLOCK',
            '00000000-0000-0000-0000-000000000013',
            now()
        );

        RAISE EXCEPTION
            'Expected reserved_inventory_one_active_claim_uq to reject duplicate';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE
                'PASS: second active reserved-seat claim rejected';
    END;
END;
$$;

DO $$
BEGIN
    BEGIN
        UPDATE ga_shared_inventory
        SET available_quantity = 9
        WHERE ga_pool_id =
            '00000000-0000-0000-0000-000000000008';

        PERFORM tktsync_assert_ga_pool_balance(
            '00000000-0000-0000-0000-000000000008'
        );

        RAISE EXCEPTION
            'Expected GA pool imbalance to be rejected';
    EXCEPTION
        WHEN check_violation THEN
            RAISE NOTICE
                'PASS: malformed GA pool balance rejected';
    END;
END;
$$;

DO $$
BEGIN
    BEGIN
        UPDATE ticket_entitlements
        SET
            status = 'VOIDED',
            voided_at = now()
        WHERE id =
            '00000000-0000-0000-0000-000000000024';

        PERFORM tktsync_assert_qr_ticket_state(
            '00000000-0000-0000-0000-000000000024'
        );

        RAISE EXCEPTION
            'Expected VOIDED Ticket with ACTIVE QR to be rejected';
    EXCEPTION
        WHEN check_violation THEN
            RAISE NOTICE
                'PASS: active QR on voided Ticket rejected';
    END;
END;
$$;

INSERT INTO idempotency_operations (
    id,
    scope_kind,
    app_user_id,
    operation_type,
    idempotency_key,
    request_hash,
    execution_state,
    result_code,
    completed_at
) VALUES
(
    '00000000-0000-0000-0000-000000000030',
    'USER',
    '00000000-0000-0000-0000-000000000001',
    'SCAN',
    'm1-scan-1',
    decode('01', 'hex'),
    'SUCCEEDED',
    'ADMITTED',
    now()
),
(
    '00000000-0000-0000-0000-000000000031',
    'USER',
    '00000000-0000-0000-0000-000000000001',
    'SCAN',
    'm1-scan-2',
    decode('02', 'hex'),
    'SUCCEEDED',
    'ALREADY_ADMITTED',
    now()
);

INSERT INTO scan_attempts (
    id,
    event_id,
    scanner_user_id,
    ticket_entitlement_id,
    qr_credential_id,
    idempotency_operation_id,
    result,
    occurred_at
) VALUES (
    '00000000-0000-0000-0000-000000000032',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000024',
    '00000000-0000-0000-0000-000000000025',
    '00000000-0000-0000-0000-000000000030',
    'ADMITTED',
    now()
);

INSERT INTO admissions (
    id,
    event_id,
    ticket_entitlement_id,
    scan_attempt_id,
    status,
    admitted_at
) VALUES (
    '00000000-0000-0000-0000-000000000033',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000024',
    '00000000-0000-0000-0000-000000000032',
    'ACTIVE',
    now()
);

INSERT INTO scan_attempts (
    id,
    event_id,
    scanner_user_id,
    ticket_entitlement_id,
    qr_credential_id,
    idempotency_operation_id,
    result,
    occurred_at
) VALUES (
    '00000000-0000-0000-0000-000000000034',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000024',
    '00000000-0000-0000-0000-000000000025',
    '00000000-0000-0000-0000-000000000031',
    'ALREADY_ADMITTED',
    now()
);

DO $$
BEGIN
    BEGIN
        INSERT INTO admissions (
            event_id,
            ticket_entitlement_id,
            scan_attempt_id,
            status,
            admitted_at
        ) VALUES (
            '00000000-0000-0000-0000-000000000004',
            '00000000-0000-0000-0000-000000000024',
            '00000000-0000-0000-0000-000000000034',
            'ACTIVE',
            now()
        );

        RAISE EXCEPTION
            'Expected second active Admission to be rejected';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE
                'PASS: second active Admission rejected';
    END;
END;
$$;

ROLLBACK;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND indexname = 'reserved_inventory_one_active_claim_uq'
    ) THEN
        RAISE EXCEPTION
            'Missing reserved_inventory_one_active_claim_uq';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND indexname = 'admissions_one_active_per_ticket_uq'
    ) THEN
        RAISE EXCEPTION
            'Missing admissions_one_active_per_ticket_uq';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'ct_validate_reserved_claim'
          AND NOT tgisinternal
    ) THEN
        RAISE EXCEPTION
            'Missing ct_validate_reserved_claim';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'ct_validate_ga_pool_balance_shared'
          AND NOT tgisinternal
    ) THEN
        RAISE EXCEPTION
            'Missing GA pool balance constraint trigger';
    END IF;

    RAISE NOTICE 'M1 schema invariant verification COMPLETE';
END;
$$;

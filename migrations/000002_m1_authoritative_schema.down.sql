BEGIN;

DROP VIEW IF EXISTS v_ga_inventory_current_summary;
DROP VIEW IF EXISTS v_reserved_inventory_current_state;

DROP TABLE IF EXISTS webhook_delivery_attempts CASCADE;
DROP TABLE IF EXISTS webhook_deliveries CASCADE;
DROP TABLE IF EXISTS partner_webhook_subscriptions CASCADE;
DROP TABLE IF EXISTS partner_webhook_signing_secrets CASCADE;
DROP TABLE IF EXISTS partner_webhook_endpoints CASCADE;

DROP TABLE IF EXISTS outbox_events CASCADE;
DROP TABLE IF EXISTS audit_events CASCADE;

DROP TABLE IF EXISTS admissions CASCADE;
DROP TABLE IF EXISTS scan_attempts CASCADE;

DROP TABLE IF EXISTS reserved_inventory_claims CASCADE;

DROP TABLE IF EXISTS qr_credentials CASCADE;
DROP TABLE IF EXISTS ticket_attendee_details CASCADE;
DROP TABLE IF EXISTS ticket_entitlements CASCADE;
DROP TABLE IF EXISTS non_public_issuance_items CASCADE;
DROP TABLE IF EXISTS non_public_issuances CASCADE;
DROP TABLE IF EXISTS sale_items CASCADE;
DROP TABLE IF EXISTS sales CASCADE;

DROP TABLE IF EXISTS checkout_attempts CASCADE;
DROP TABLE IF EXISTS reservation_items CASCADE;
DROP TABLE IF EXISTS reservations CASCADE;

DROP TABLE IF EXISTS idempotency_operations CASCADE;

DROP TABLE IF EXISTS ga_allocation_buckets CASCADE;
DROP TABLE IF EXISTS ga_block_buckets CASCADE;
DROP TABLE IF EXISTS allocation_reserved_units CASCADE;
DROP TABLE IF EXISTS block_reserved_units CASCADE;
DROP TABLE IF EXISTS allocations CASCADE;
DROP TABLE IF EXISTS blocks CASCADE;
DROP TABLE IF EXISTS inventory_restrictions CASCADE;

DROP TABLE IF EXISTS buyer_selection_sessions CASCADE;
DROP TABLE IF EXISTS partner_event_access CASCADE;
DROP TABLE IF EXISTS event_staff_assignments CASCADE;

DROP TABLE IF EXISTS ga_shared_inventory CASCADE;
DROP TABLE IF EXISTS ga_inventory_pools CASCADE;
DROP TABLE IF EXISTS reserved_inventory_units CASCADE;
DROP TABLE IF EXISTS event_sections CASCADE;
DROP TABLE IF EXISTS event_price_tiers CASCADE;
DROP TABLE IF EXISTS event_layout_snapshots CASCADE;
DROP TABLE IF EXISTS event_transaction_policies CASCADE;
DROP TABLE IF EXISTS events CASCADE;

DROP TABLE IF EXISTS partner_credentials CASCADE;
DROP TABLE IF EXISTS partners CASCADE;

DROP TABLE IF EXISTS platform_user_roles CASCADE;
DROP TABLE IF EXISTS app_users CASCADE;

DROP TABLE IF EXISTS venue_layout_ga_zones CASCADE;
DROP TABLE IF EXISTS venue_layout_seats CASCADE;
DROP TABLE IF EXISTS venue_layout_tables CASCADE;
DROP TABLE IF EXISTS venue_layout_rows CASCADE;
DROP TABLE IF EXISTS venue_layout_sections CASCADE;
DROP TABLE IF EXISTS venue_layout_versions CASCADE;
DROP TABLE IF EXISTS venues CASCADE;

DROP FUNCTION IF EXISTS ct_prevent_immutable_fact_update_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_protect_qr_identity_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_protect_ticket_identity_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_protect_reservation_item_snapshot_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_protect_live_event_inventory_identity_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_protect_published_layout_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_event_snapshot_materialization_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_layout_component_scope_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_qr_ticket_state_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_origin_ticket_cardinality_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_ticket_origin_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_issuance_item_source_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_non_public_issuance_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_sale_item_snapshot_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_enforce_transaction_currency_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_reserved_claim_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_ga_active_reservations_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_ga_pool_balance_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_allocation_release_destination_fn() CASCADE;
DROP FUNCTION IF EXISTS ct_validate_restriction_subtype_fn() CASCADE;

DROP FUNCTION IF EXISTS tktsync_assert_issuance_ticket_cardinality(uuid) CASCADE;
DROP FUNCTION IF EXISTS tktsync_assert_sale_ticket_cardinality(uuid) CASCADE;
DROP FUNCTION IF EXISTS tktsync_assert_qr_ticket_state(uuid) CASCADE;
DROP FUNCTION IF EXISTS tktsync_assert_ga_active_reservations(uuid) CASCADE;
DROP FUNCTION IF EXISTS tktsync_assert_ga_pool_balance(uuid) CASCADE;
DROP FUNCTION IF EXISTS tktsync_event_has_protected_history(uuid) CASCADE;

COMMIT;

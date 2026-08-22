ALTER TABLE ticket_entitlements
    DROP CONSTRAINT ticket_entitlements_inventory_release_ck,
    DROP COLUMN inventory_release_destination_allocation_id,
    DROP COLUMN inventory_release_destination_kind,
    DROP COLUMN inventory_release_reason,
    DROP COLUMN inventory_released_at;

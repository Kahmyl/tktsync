ALTER TABLE ticket_entitlements
    ADD COLUMN inventory_released_at timestamptz,
    ADD COLUMN inventory_release_reason text,
    ADD COLUMN inventory_release_destination_kind text,
    ADD COLUMN inventory_release_destination_allocation_id uuid
        REFERENCES allocations(restriction_id) ON DELETE RESTRICT;

ALTER TABLE ticket_entitlements
    ADD CONSTRAINT ticket_entitlements_inventory_release_ck CHECK (
        (
            inventory_released_at IS NULL
            AND inventory_release_reason IS NULL
            AND inventory_release_destination_kind IS NULL
            AND inventory_release_destination_allocation_id IS NULL
        )
        OR
        (
            status = 'VOIDED'
            AND inventory_released_at IS NOT NULL
            AND inventory_release_reason IS NOT NULL
            AND btrim(inventory_release_reason) <> ''
            AND (
                (
                    inventory_release_destination_kind = 'SHARED'
                    AND inventory_release_destination_allocation_id IS NULL
                )
                OR
                (
                    inventory_release_destination_kind = 'ALLOCATION'
                    AND inventory_release_destination_allocation_id IS NOT NULL
                )
            )
        )
    );

COMMENT ON COLUMN ticket_entitlements.inventory_released_at IS
    'One-time explicit release of current SOLD/ISSUED capacity; entitlement and origin history remain immutable.';

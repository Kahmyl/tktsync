DROP TRIGGER IF EXISTS ct_guard_ticket_inventory_release_leaf
ON ticket_entitlements;

DROP FUNCTION IF EXISTS ct_guard_ticket_inventory_release_leaf_fn();

DROP TRIGGER IF EXISTS ct_guard_ticket_replacement_after_release
ON ticket_entitlements;

DROP FUNCTION IF EXISTS ct_guard_ticket_replacement_after_release_fn();

CREATE OR REPLACE FUNCTION ct_guard_ticket_replacement_after_release_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_parent_released_at timestamptz;
BEGIN
    IF NEW.replaces_ticket_entitlement_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT inventory_released_at
    INTO v_parent_released_at
    FROM ticket_entitlements
    WHERE id = NEW.replaces_ticket_entitlement_id
    FOR KEY SHARE;

    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF v_parent_released_at IS NOT NULL THEN
        RAISE EXCEPTION
            'Replacement Ticket cannot descend from inventory-released entitlement %',
            NEW.replaces_ticket_entitlement_id
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS ct_guard_ticket_replacement_after_release
ON ticket_entitlements;

CREATE TRIGGER ct_guard_ticket_replacement_after_release
BEFORE INSERT OR UPDATE OF replaces_ticket_entitlement_id
ON ticket_entitlements
FOR EACH ROW
EXECUTE FUNCTION ct_guard_ticket_replacement_after_release_fn();

CREATE OR REPLACE FUNCTION ct_guard_ticket_inventory_release_leaf_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_child_id uuid;
BEGIN
    IF NEW.inventory_released_at IS NULL
       OR OLD.inventory_released_at IS NOT NULL
    THEN
        RETURN NEW;
    END IF;

    SELECT id
    INTO v_child_id
    FROM ticket_entitlements
    WHERE replaces_ticket_entitlement_id = NEW.id
    LIMIT 1
    FOR KEY SHARE;

    IF FOUND THEN
        RAISE EXCEPTION
            'Inventory may only be re-released from the leaf Ticket entitlement; child % exists',
            v_child_id
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS ct_guard_ticket_inventory_release_leaf
ON ticket_entitlements;

CREATE TRIGGER ct_guard_ticket_inventory_release_leaf
BEFORE UPDATE OF inventory_released_at
ON ticket_entitlements
FOR EACH ROW
EXECUTE FUNCTION ct_guard_ticket_inventory_release_leaf_fn();

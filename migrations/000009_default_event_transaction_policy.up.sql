INSERT INTO event_transaction_policies (
    event_id,
    hold_duration_seconds,
    checkout_protection_seconds,
    payment_retry_seconds,
    reconciliation_seconds,
    max_reservation_lifetime_seconds,
    max_hold_quantity,
    max_active_reservations_per_partner,
    max_active_reservations_per_buyer_session,
    allow_voided_inventory_rerelease,
    created_at,
    updated_at
)
SELECT
    events.id,
    600,
    120,
    300,
    600,
    1800,
    12,
    500,
    3,
    false,
    clock_timestamp(),
    clock_timestamp()
FROM events
WHERE NOT EXISTS (
    SELECT 1
    FROM event_transaction_policies
    WHERE event_transaction_policies.event_id = events.id
);

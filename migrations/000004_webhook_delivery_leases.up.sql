ALTER TABLE webhook_deliveries
    ADD COLUMN leased_by uuid,
    ADD COLUMN lease_until timestamptz;

ALTER TABLE webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_lease_ck CHECK (
        (leased_by IS NULL) = (lease_until IS NULL)
    );

CREATE INDEX webhook_deliveries_claim_idx
ON webhook_deliveries(state, next_attempt_at, lease_until, id);

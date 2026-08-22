DROP INDEX webhook_deliveries_claim_idx;
ALTER TABLE webhook_deliveries
    DROP CONSTRAINT webhook_deliveries_lease_ck,
    DROP COLUMN lease_until,
    DROP COLUMN leased_by;

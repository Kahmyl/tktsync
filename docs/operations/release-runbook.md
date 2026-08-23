# MVP Release Runbook

This runbook covers deployment and recovery operations for the authoritative PostgreSQL-backed MVP. Reporting, realtime delivery, and webhooks remain derived or advisory and are never correctness authorities.

## Deployment

1. Produce immutable API, worker, Admin, selector, and scanner artifacts from the same revision.
2. Run `make verify-release` against an isolated PostgreSQL instance before promotion.
3. Back up PostgreSQL, then apply the ordered migrations once with `migrate ... up`. Never edit an applied migration.
4. Deploy the API with all required versioned keyrings and encrypted webhook-secret keys available. Keep the preceding verification key during the documented overlap window.
5. Require `/health` and database-backed `/ready` success before routing traffic. Start workers only after schema migration succeeds.
6. Smoke-test Partner authentication, availability, one reversible hold/release, Admin reporting, and scanner authority.

## Rollback

Roll back application artifacts to the preceding compatible revision. Do not automatically run destructive down migrations. If a schema rollback is genuinely required, stop mutations, take another backup, review the exact down migration, and execute it as a separately approved operation.

## Backup

Use encrypted PostgreSQL-native backups with checksums and retention appropriate to audit, Sale, Ticket, and Admission history. At minimum, capture a pre-deployment backup and scheduled production backups. Restrict backup access like production database access; backups contain identifiers and potentially permitted attendee metadata.

Example logical capture, with credentials supplied through the deployment secret manager:

```sh
pg_dump --format=custom --no-owner --file=tktsync.dump "$DATABASE_URL"
```

## Recovery

Restore into a new, isolated database rather than overwriting the live authority. Verify the backup checksum, restore, run read-only consistency checks for Events, Reservations, Sales, Tickets, Admissions, audit events, outbox facts, and migration version, then point a non-production API at the restored database for smoke testing. Production cutover requires an explicit incident decision and a final write freeze or authoritative replication position.

```sh
createdb tktsync_recovery
pg_restore --exit-on-error --no-owner --dbname=tktsync_recovery tktsync.dump
```

## Key rotation

Rotate selection, Reservation, QR, webhook-encryption, and human-auth verification keys independently. Publish the new active version while retaining required historical verification/decryption keys, verify old and new material during the overlap, then retire old versions only after their governed lifetime. Partner credentials and webhook signing secrets use their explicit rotate/revoke commands; never log raw material.

## Alert response

- Database unavailable: stop accepting authoritative mutations and investigate PostgreSQL health; never fall back to a cache.
- Reconciliation overdue: restore worker capacity and inspect bounded reconciliation records without extending deadlines.
- Outbox/webhook backlog: restore dispatch capacity; idempotent consumers and stable delivery identities prevent duplicate business effects.
- Admission rejection or auth anomaly spike: preserve scan/audit evidence, verify Event and staff scope, and inspect credential status/key versions.
- Realtime unavailable: clients re-fetch authoritative state; realtime failure alone does not change inventory or transaction outcomes.

Every incident action that changes business state must use an authorized domain command and leave audit evidence. Direct SQL repair requires an exceptional, reviewed recovery procedure.

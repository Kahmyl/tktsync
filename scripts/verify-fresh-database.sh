#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
POSTGRES_PORT=${POSTGRES_PORT:-55432}
FRESH_DB="tktsync_fresh_verification_$$"
POSTGRES_IMAGE=${POSTGRES_IMAGE:-postgres:17-alpine}
MIGRATE_IMAGE=${MIGRATE_IMAGE:-migrate/migrate:v4.18.3}
FRESH_URL="postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/${FRESH_DB}?sslmode=disable"

psql_admin() {
  docker run --rm --network host -e PGPASSWORD=tktsync "$POSTGRES_IMAGE" \
    psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -p "$POSTGRES_PORT" -U tktsync -d postgres "$@"
}

cleanup() {
  psql_admin -c "DROP DATABASE IF EXISTS ${FRESH_DB} WITH (FORCE)" >/dev/null
}
trap cleanup EXIT HUP INT TERM

echo "Creating disposable verification database ${FRESH_DB}..."
psql_admin -c "CREATE DATABASE ${FRESH_DB}"

echo "Applying the complete migration chain to the disposable database..."
docker run --rm --network host -v "$REPO_DIR/migrations:/migrations:ro" "$MIGRATE_IMAGE" \
  -path=/migrations -database "$FRESH_URL" up

docker run --rm --network host -e PGPASSWORD=tktsync "$POSTGRES_IMAGE" \
  psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -p "$POSTGRES_PORT" -U tktsync -d "$FRESH_DB" \
  -c "SELECT 1 / CASE WHEN COUNT(*)=1 THEN 1 ELSE 0 END AS migrations_ok FROM schema_migrations WHERE version=7 AND dirty=false" \
  -c "SELECT 1 / CASE WHEN COUNT(*)=4 THEN 1 ELSE 0 END AS tables_ok FROM (VALUES ('events'),('audit_events'),('sales'),('admissions')) AS required(name) WHERE to_regclass('public.' || name) IS NOT NULL"

echo "Running configuration, reporting, and release semantic certification on the fresh schema..."
cd "$REPO_DIR/backend"
DATABASE_URL="$FRESH_URL" GOCACHE=${GOCACHE:-/tmp/tktsync-go-build} \
  go test -tags=integration ./internal/event ./internal/adminapi ./internal/reservation \
  -run '^(TestEventConfigurationFlow|TestAdminLifecycleAuthorizationReasonAuditAndIdempotency|TestReporting.*|TestDatabaseUnavailableFailsAuthoritativeMutationClosed|TestHoldVersusBlockHasOneAuthoritativeWinner|TestPartnerDisableVersusAcquisitionUsesPartnerGate|TestConfirmationVersusCancellationOrdering|TestConfirmationExpiryGuardUsesPostLockDatabaseTime|TestCancellationCleanupIsBoundedAndStateAware|TestVoidAndReissueVersusScanOrdering|TestRealtimeConcurrentConnectionsRemainAdvisory)$' -count=1

echo "Fresh-database migration and semantic certification COMPLETE."

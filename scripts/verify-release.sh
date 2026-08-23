#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export POSTGRES_PORT=${POSTGRES_PORT:-55432}
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

echo "Running backend static verification..."
cd "$REPO_DIR/backend"
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go build -o /tmp/tktsync-release-api ./cmd/api
go build -o /tmp/tktsync-release-worker ./cmd/worker

echo "Running contract and frontend verification..."
cd "$REPO_DIR"
jq empty openapi/tktsync.v1.json
node scripts/check-release-contract.mjs
node scripts/check-selection-contract.mjs
node scripts/check-reporting-contract.mjs
pnpm api:routes
pnpm api:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm format:check

echo "Running Partner and concurrency certification..."
"$REPO_DIR/scripts/certify-partner-integration.sh"

cd "$REPO_DIR/backend"
go test -tags=integration ./internal/adminapi \
  -run '^TestAdminLifecycleAuthorizationReasonAuditAndIdempotency$' \
  -count=1
go test -tags=integration ./internal/platform/database \
  -run '^TestRunnerRetriesAnActualPostgreSQLDeadlock$' \
  -count=1
go test -tags=integration ./internal/reservation \
  -run '^(TestReporting.*|TestSelection.*|TestAdmission.*|TestHoldVersusBlockHasOneAuthoritativeWinner|TestPartnerDisableVersusAcquisitionUsesPartnerGate|TestConfirmationVersusCancellationOrdering|TestConfirmationExpiryGuardUsesPostLockDatabaseTime|TestCancellationCleanupIsBoundedAndStateAware|TestVoidAndReissueVersusScanOrdering)$' \
  -count=1

echo "Running clean-schema certification exactly once..."
cd "$REPO_DIR"
"$REPO_DIR/scripts/verify-fresh-database.sh"

git diff --check
echo "TktSync MVP RELEASE GATE COMPLETE."

#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export POSTGRES_PORT=${POSTGRES_PORT:-55432}
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

echo "Running Partner, E2E, concurrency, load, and async certification..."
"$REPO_DIR/scripts/certify-partner-integration.sh"

echo "Running lifecycle authorization and PostgreSQL retry certification..."
cd "$REPO_DIR/backend"
go test -tags=integration ./internal/adminapi -run '^TestAdminLifecycleAuthorizationReasonAuditAndIdempotency$' -count=1 -v
go test -tags=integration ./internal/platform/database -run '^TestRunnerRetriesAnActualPostgreSQLDeadlock$' -count=1 -v
go test ./...
go vet ./...
go build -o /tmp/tktsync-release-api ./cmd/api
go build -o /tmp/tktsync-release-worker ./cmd/worker
test -z "$(gofmt -l .)"

echo "Running release security, contract, and generated-client assertions..."
cd "$REPO_DIR"
jq empty openapi/tktsync.v1.json
node scripts/check-release-contract.mjs
pnpm api:routes
pnpm api:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm format:check

echo "Running the clean-schema migration certification..."
"$REPO_DIR/scripts/verify-fresh-database.sh"

echo "Running reporting and every prior certification gate..."
POSTGRES_PORT="$POSTGRES_PORT" DATABASE_URL="$DATABASE_URL" "$REPO_DIR/scripts/verify-reporting.sh"

git diff --check
echo "TktSync MVP RELEASE GATE COMPLETE."

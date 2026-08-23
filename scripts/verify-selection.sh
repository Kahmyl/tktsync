#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT:-5432}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

echo "Running selection authority and handoff certification..."
cd "$REPO_DIR/backend"
go test -tags=integration ./internal/reservation -run '^TestSelection' -count=1 -v
go test ./...
go vet ./...
test -z "$(gofmt -l .)"

echo "Running selection OpenAPI and generated-client certification..."
cd "$REPO_DIR"
jq empty openapi/tktsync.v1.json
node scripts/check-selection-contract.mjs
pnpm api:routes
pnpm api:check

echo "Running all React product-surface gates..."
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm format:check

echo "Running asynchronous delivery and prior regression certification..."
POSTGRES_PORT=${POSTGRES_PORT:-5432} "$REPO_DIR/scripts/verify-async-delivery.sh"
echo "Selection verification COMPLETE."

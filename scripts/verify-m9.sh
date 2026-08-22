#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT:-5432}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

echo "Running M9 selection authority and handoff certification..."
cd "$REPO_DIR/backend"
go test -tags=integration ./internal/reservation -run '^TestM9' -count=1 -v
go test ./...
go vet ./...
test -z "$(gofmt -l .)"

echo "Running M9 OpenAPI and generated-client certification..."
cd "$REPO_DIR"
jq empty openapi/tktsync.v1.json
pnpm api:routes
pnpm api:check

echo "Running all React product-surface gates..."
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm format:check

echo "Running M8 and prior regression certification..."
POSTGRES_PORT=${POSTGRES_PORT:-5432} "$REPO_DIR/scripts/verify-m8.sh"
echo "M9 verification COMPLETE."

#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT:-5432}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

cd "$REPO_DIR/backend"
go test -tags=integration ./internal/reservation -run '^TestAsyncDelivery' -count=1 -v
go test ./...
go vet ./...

cd "$REPO_DIR"
jq empty openapi/tktsync.v1.json
pnpm api:routes
pnpm api:check
pnpm --filter @tktsync/api-client lint
pnpm --filter @tktsync/api-client typecheck
pnpm --filter @tktsync/api-client test
pnpm --filter @tktsync/api-client build

POSTGRES_PORT=${POSTGRES_PORT:-5432} "$REPO_DIR/scripts/verify-admissions.sh"

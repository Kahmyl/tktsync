#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"
export DATABASE_URL
export GOCACHE="${GOCACHE:-/tmp/tktsync-go-build}"

echo "Running M7 authoritative admission and scanner certification..."
(
  cd "$ROOT/backend"
  go test -tags=integration ./internal/reservation -run '^TestM7' -count=1 -v
  go test ./...
  go vet ./...
)

echo "Running M7 contract and generated-client certification..."
jq empty "$ROOT/openapi/tktsync.v1.json"
(cd "$ROOT" && pnpm api:routes && pnpm api:check)
(cd "$ROOT" && pnpm --filter @tktsync/api-client lint && pnpm --filter @tktsync/api-client typecheck && pnpm --filter @tktsync/api-client test && pnpm --filter @tktsync/api-client build)

echo "Running M6 and prior regression certification..."
POSTGRES_PORT="$POSTGRES_PORT" DATABASE_URL="$DATABASE_URL" "$ROOT/scripts/verify-m6.sh"

echo "M7 verification COMPLETE."

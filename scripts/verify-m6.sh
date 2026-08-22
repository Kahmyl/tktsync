#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"
export DATABASE_URL
export GOCACHE="${GOCACHE:-/tmp/tktsync-go-build}"

echo "Running M6 confirmation, issuance, Ticket lifecycle, and inventory release certification..."
(
  cd "$ROOT/backend"
  go test -tags=integration ./internal/reservation ./internal/allocation ./internal/partnerapi ./internal/audit -run '^(TestM6|TestAudit)' -count=1
)

echo "Running OpenAPI parse, route parity, and generated-client certification..."
jq empty "$ROOT/openapi/tktsync.v1.json"
(cd "$ROOT" && pnpm api:routes && pnpm api:check)
(cd "$ROOT" && pnpm --filter @tktsync/api-client lint && pnpm --filter @tktsync/api-client typecheck && pnpm --filter @tktsync/api-client test && pnpm --filter @tktsync/api-client build)

echo "Running M5 regression certification..."
POSTGRES_PORT="$POSTGRES_PORT" DATABASE_URL="$DATABASE_URL" "$ROOT/scripts/verify-m5.sh"

echo "M6 verification COMPLETE."

#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"

export DATABASE_URL

echo "Running Event configuration-domain certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/event \
    -run '^TestEventConfigurationFlow$' \
    -count=1 \
    -v
)

echo
echo "Running Admin HTTP/idempotency certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/adminapi \
    -run '^TestAdminHTTPIdempotencyAndCredentialReplay$' \
    -count=1 \
    -v
)

echo
echo "Running nested-transaction support tests..."

(
  cd "$ROOT/backend"
  go test \
    ./internal/platform/database \
    -count=1
)

echo
echo "Verifying Event configuration implementation boundaries..."

test -f "$ROOT/backend/internal/venue/service.go"
test -f "$ROOT/backend/internal/event/service.go"
test -f "$ROOT/backend/internal/event/configuration.go"
test -f "$ROOT/backend/internal/partner/service.go"
test -f "$ROOT/backend/internal/adminapi/handler.go"
test -f "$ROOT/backend/internal/adminapi/executor.go"
test -f "$ROOT/backend/internal/adminapi/replay_protector.go"

if grep -R --line-number --fixed-strings '"credential":"' \
  "$ROOT/backend/internal" \
  | grep -v handler.go \
  | grep -v handler_integration_test.go; then
  echo "Unexpected raw credential persistence/logging pattern found."
  exit 1
fi

echo
echo "Event configuration verification COMPLETE."

#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"

export DATABASE_URL

echo "Running M2 Go unit tests..."
(
  cd "$ROOT/backend"
  go test ./...
)

echo
echo "Running M2 Go vet..."
(
  cd "$ROOT/backend"
  go vet ./...
)

echo
echo "Running M2 PostgreSQL idempotency certification..."
(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/idempotency \
    -count=1
)

echo
echo "Running M2 audit/outbox atomicity certification..."
(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/audit \
    -count=1
)

echo
echo "M2 platform package inventory:"
find "$ROOT/backend/internal" \
  -maxdepth 3 \
  -type f \
  \( \
    -path '*/auth/*.go' \
    -o -path '*/audit/*.go' \
    -o -path '*/outbox/*.go' \
    -o -path '*/idempotency/*.go' \
    -o -path '*/platform/apierror/*.go' \
    -o -path '*/platform/publicid/*.go' \
    -o -path '*/platform/database/*.go' \
  \) \
  | sort

echo
echo "M2 verification COMPLETE."

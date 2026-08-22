#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"

export DATABASE_URL

echo "Running M4 restriction/allocation/availability certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/allocation \
    -run '^TestM4RestrictionsAllocationsAndAvailability$' \
    -count=1 \
    -v
)

echo
echo "Running M4 Admin/Partner HTTP certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/partnerapi \
    -run '^TestM4AdminAndPartnerHTTP$' \
    -count=1 \
    -v
)

echo
echo "Running M3 regression certification..."
"$ROOT/scripts/verify-m3.sh"

echo
echo "Running M2 regression certification..."
"$ROOT/scripts/verify-m2.sh"

echo
echo "Verifying M4 implementation..."

test -f "$ROOT/backend/internal/allocation/service.go"
test -f "$ROOT/backend/internal/allocation/reclassify.go"
test -f "$ROOT/backend/internal/inventory/availability.go"
test -f "$ROOT/backend/internal/inventory/offer.go"
test -f "$ROOT/backend/internal/inventory/capacity.go"
test -f "$ROOT/backend/internal/adminapi/m4.go"
test -f "$ROOT/backend/internal/partnerapi/handler.go"

echo
echo
echo "Running M4-C OpenAPI/generated-client certification..."
"$ROOT/scripts/verify-m4c.sh"

echo
echo "M4 verification COMPLETE."

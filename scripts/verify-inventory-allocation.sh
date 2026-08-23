#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"

export DATABASE_URL

echo "Running restriction, allocation, and availability certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/allocation \
    -run '^TestRestrictionsAllocationsAndAvailability$' \
    -count=1 \
    -v
)

echo
echo "Running Admin/Partner allocation HTTP certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/partnerapi \
    -run '^TestAdminAndPartnerAllocationHTTP$' \
    -count=1 \
    -v
)

echo
echo "Running Event configuration regression certification..."
"$ROOT/scripts/verify-event-configuration.sh"

echo
echo "Running platform foundation regression certification..."
"$ROOT/scripts/verify-platform-foundation.sh"

echo
echo "Verifying inventory and allocation implementation..."

test -f "$ROOT/backend/internal/allocation/service.go"
test -f "$ROOT/backend/internal/allocation/reclassify.go"
test -f "$ROOT/backend/internal/inventory/availability.go"
test -f "$ROOT/backend/internal/inventory/offer.go"
test -f "$ROOT/backend/internal/inventory/capacity.go"
test -f "$ROOT/backend/internal/adminapi/inventory_allocation.go"
test -f "$ROOT/backend/internal/partnerapi/handler.go"

echo
echo
echo "Running API contract OpenAPI/generated-client certification..."
"$ROOT/scripts/verify-api-contract.sh"

echo
echo "Inventory and allocation verification COMPLETE."

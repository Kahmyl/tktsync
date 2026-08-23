#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://tktsync:tktsync@localhost:${POSTGRES_PORT}/tktsync?sslmode=disable}"

export DATABASE_URL

echo "Running Reservation lifecycle certification..."

(
  cd "$ROOT/backend"
  go test \
    -tags=integration \
    ./internal/reservation \
    -run '^TestReservation' \
    -count=1 \
    -v
)

echo
echo "Running inventory and allocation regression certification..."
"$ROOT/scripts/verify-inventory-allocation.sh"

echo
echo "Checking Reservation implementation..."

test -f "$ROOT/backend/internal/reservation/types.go"
test -f "$ROOT/backend/internal/reservation/token.go"
test -f "$ROOT/backend/internal/reservation/inventory.go"
test -f "$ROOT/backend/internal/reservation/service.go"
test -f "$ROOT/backend/internal/reservation/lifecycle.go"
test -f "$ROOT/backend/internal/reservation/materializer.go"

grep -q "rsv1" "$ROOT/backend/internal/reservation/token.go"
grep -q "TokenHash" "$ROOT/backend/internal/reservation/token.go"
grep -q "FOR UPDATE" "$ROOT/backend/internal/reservation/inventory.go"
grep -q "active_reserved_quantity" "$ROOT/backend/internal/reservation/inventory.go"
grep -q "PAYMENT_RETRY" "$ROOT/backend/internal/reservation/lifecycle.go"
grep -q "RECONCILING" "$ROOT/backend/internal/reservation/lifecycle.go"
grep -q "clock_timestamp" "$ROOT/backend/internal/reservation/materializer.go"
grep -q "reservationMaterializer" "$ROOT/backend/cmd/worker/main.go"

if find "$ROOT/backend/internal/ticketing" -type f ! -name '.gitkeep' | grep -q .; then
  echo "Reservation verification found unexpected Ticketing-domain files."
  exit 1
fi

echo
echo "Reservation verification COMPLETE."

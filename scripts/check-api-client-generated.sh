#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
GENERATED="$ROOT/packages/api-client/src/generated/schema.ts"
TMP="$(mktemp)"

if [ ! -f "$GENERATED" ]; then
  echo "Generated API schema is missing: $GENERATED"
  rm -f "$TMP"
  exit 1
fi

cp "$GENERATED" "$TMP"

(
  cd "$ROOT"
  pnpm --filter @tktsync/api-client generate >/dev/null
)

if ! cmp -s "$TMP" "$GENERATED"; then
  echo "Generated API client is out of date."
  diff -u "$TMP" "$GENERATED" || true
  rm -f "$TMP"
  exit 1
fi

rm -f "$TMP"
echo "Generated API client is current."

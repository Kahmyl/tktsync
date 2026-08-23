#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

jq -e '.openapi == "3.1.0"' openapi/tktsync.v1.json >/dev/null
pnpm api:routes

pnpm api:check
pnpm --filter @tktsync/api-client lint
pnpm --filter @tktsync/api-client typecheck
pnpm --filter @tktsync/api-client test
pnpm --filter @tktsync/api-client build

echo "API contract OpenAPI/generated-client certification COMPLETE."

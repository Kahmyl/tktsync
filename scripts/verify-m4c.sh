#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

python3 <<'PY'
from pathlib import Path
import json
import re
import sys

spec_path = Path("openapi/tktsync.v1.json")
spec = json.loads(spec_path.read_text(encoding="utf-8"))

if spec.get("openapi") != "3.1.0":
    raise SystemExit("OpenAPI document is not 3.1.0")

implemented = set()

for path in [
    Path("backend/internal/adminapi/handler.go"),
    Path("backend/internal/adminapi/m4.go"),
    Path("backend/internal/partnerapi/handler.go"),
]:
    text = path.read_text(encoding="utf-8")
    for method, route in re.findall(
        r'"(GET|POST|PATCH|PUT|DELETE) (/api/v1/(?:admin|partner)/[^"]+)"',
        text,
    ):
        implemented.add((method, route))

documented = set()

for route, path_item in spec["paths"].items():
    for method in ["get", "post", "patch", "put", "delete"]:
        operation = path_item.get(method)
        if operation is None:
            continue

        documented.add((method.upper(), route))

        if not operation.get("operationId"):
            raise SystemExit(f"{method.upper()} {route} has no operationId")

        security = operation.get("security", [])

        if route.startswith("/api/v1/admin/"):
            if {"AdminBearer": []} not in security:
                raise SystemExit(f"{method.upper()} {route} lacks AdminBearer security")

            if method != "get":
                names = {
                    p.get("name")
                    for p in operation.get("parameters", [])
                    if p.get("in") == "header"
                }

                if "Idempotency-Key" not in names:
                    raise SystemExit(
                        f"{method.upper()} {route} lacks Idempotency-Key"
                    )

        if route.startswith("/api/v1/partner/"):
            if {"PartnerBearer": []} not in security:
                raise SystemExit(f"{method.upper()} {route} lacks PartnerBearer security")

missing = implemented - documented
extra = documented - implemented

if missing:
    print("Implemented routes missing from OpenAPI:")
    for item in sorted(missing):
        print(item)
    sys.exit(1)

if extra:
    print("OpenAPI routes without an implemented M3/M4 route:")
    for item in sorted(extra):
        print(item)
    sys.exit(1)

print(f"OpenAPI route parity certified: {len(documented)} operations.")
PY

pnpm api:check
pnpm --filter @tktsync/api-client lint
pnpm --filter @tktsync/api-client typecheck
pnpm --filter @tktsync/api-client test
pnpm --filter @tktsync/api-client build

echo "M4-C OpenAPI/generated-client certification COMPLETE."

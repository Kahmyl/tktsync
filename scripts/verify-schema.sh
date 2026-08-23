#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SQL="$ROOT/tests/integration/schema_invariants.sql"

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

if command -v psql >/dev/null 2>&1; then
  if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "DATABASE_URL is required when using local psql."
    exit 1
  fi

  echo "Running Schema verification using local psql..."
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$SQL"
  exit 0
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  services="$(docker compose config --services)"

  service=""
  for candidate in postgres db database; do
    if printf '%s\n' "$services" | grep -Fxq "$candidate"; then
      service="$candidate"
      break
    fi
  done

  if [[ -z "$service" ]]; then
    echo "Could not detect PostgreSQL Compose service."
    echo "Install psql or run tests/integration/schema_invariants.sql manually."
    exit 1
  fi

  echo "Running Schema verification through Docker Compose service: $service"

  docker compose exec -T "$service" sh -lc '
    psql \
      -v ON_ERROR_STOP=1 \
      -U "${POSTGRES_USER:-postgres}" \
      -d "${POSTGRES_DB:-tktsync}"
  ' < "$SQL"

  exit 0
fi

echo "Neither psql nor Docker Compose is available."
echo "Run this SQL manually against the migrated database:"
echo "  tests/integration/schema_invariants.sql"
exit 1

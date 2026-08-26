#!/bin/sh
set -eu

if [ -z "${DATABASE_URL:-}" ]; then
  echo "[startup] DATABASE_URL is required" >&2
  exit 1
fi

echo "[startup] running database migrations"
migrate -path=/app/migrations -database="$DATABASE_URL" up
echo "[startup] migrations complete"

auth_subject=${LOCAL_OPERATOR_AUTH_SUBJECT:-}
if [ -z "$auth_subject" ]; then
  echo "[startup] initial platform admin bootstrap skipped: LOCAL_OPERATOR_AUTH_SUBJECT is not configured"
else
  echo "[startup] bootstrapping initial platform admin"
  psql -X "$DATABASE_URL" \
    --set=ON_ERROR_STOP=1 \
    --set="auth_subject=$auth_subject" \
    --set="display_name=${LOCAL_OPERATOR_DISPLAY_NAME:-Local Platform Admin}" \
    --set="platform_admin=${LOCAL_OPERATOR_PLATFORM_ADMIN:-true}" \
    --file=/app/seeds/000001_local_operator.sql
  echo "[startup] initial platform admin ready"
fi

echo "[startup] starting API"
exec /usr/local/bin/tktsync-api

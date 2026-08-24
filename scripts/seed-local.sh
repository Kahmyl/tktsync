#!/bin/sh
set -eu

auth_subject=${LOCAL_OPERATOR_AUTH_SUBJECT:-}

if [ -z "$auth_subject" ]; then
  echo "Local operator seed skipped: LOCAL_OPERATOR_AUTH_SUBJECT is not configured."
  exit 0
fi

psql -X "${DATABASE_URL:?DATABASE_URL is required}" \
  --set=ON_ERROR_STOP=1 \
  --set="auth_subject=$auth_subject" \
  --set="display_name=${LOCAL_OPERATOR_DISPLAY_NAME:-Local Platform Admin}" \
  --set="platform_admin=${LOCAL_OPERATOR_PLATFORM_ADMIN:-true}" \
  --file=/seeds/000001_local_operator.sql

echo "Local operator application authorization seeded."

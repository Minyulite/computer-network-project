#!/bin/sh
set -eu

: "${CHAT_APP_PASSWORD:?CHAT_APP_PASSWORD is required}"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v app_password="$CHAT_APP_PASSWORD" <<'SQL'
CREATE ROLE chat_app LOGIN PASSWORD :'app_password';
CREATE DATABASE chat OWNER chat_app;
CREATE DATABASE chat_test OWNER chat_app;
SQL

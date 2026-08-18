#!/usr/bin/env bash
# Stand up a throwaway instance and probe every route's authorization
# against it. See scripts/audit-permissions.py for what is measured.
#
# Deliberately isolated from the dev stack: its own container, its own
# database, random ports, and a trap that tears both down whether the
# audit passes or fails. Nothing here can reach dev-data.
set -euo pipefail

cd "$(dirname "$0")/.."

PG_PORT=$((20000 + RANDOM % 20000))
APP_PORT=$((30000 + RANDOM % 20000))
CONTAINER="mailyard-audit-$$"
WORK=$(mktemp -d)

cleanup() {
  [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "audit: postgres on :$PG_PORT, server on :$APP_PORT"
docker run -d --rm --name "$CONTAINER" \
  -e POSTGRES_PASSWORD=audit -e POSTGRES_DB=audit \
  -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
for _ in $(seq 1 60); do
  docker exec "$CONTAINER" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done

go build -o "$WORK/mailyard" ./cmd/mailyard
go run ./cmd/mailyard export-api-spec --surface api --out "$WORK/openapi.yaml" >/dev/null

# Its OWN config file, empty, rather than the ./mailyard.yaml a developer
# has in the working tree. Everything this audit needs is set below, and a
# throwaway instance reading the dev config is not the isolation the
# header claims - it picked up dev settings and warned about removed keys.
echo '{}' >"$WORK/mailyard.yaml"

# The rate limiter buckets per credential and this sends hundreds of
# requests as a handful of them, so it would answer 429 rather than the
# authorization the audit is there to read.
export MAILYARD_DATABASE_DSN="postgres://postgres:audit@localhost:$PG_PORT/audit?sslmode=disable"
export MAILYARD_SERVER_ADDR=":$APP_PORT"
export MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY="0123456789abcdef0123456789abcdef"
export MAILYARD_AUTH_JWT_SECRET="0123456789abcdef0123456789abcdef0123456789abcdef"
export MAILYARD_AUTH_LOCAL_ENABLED="true"
export MAILYARD_AUTH_LOCAL_EMAIL="admin@example.test"
export MAILYARD_RATELIMIT_API_PER_MINUTE="1000000"
#
# --init because the database is empty and a node that was not asked to
# create a schema refuses to boot. Without it this audit could not run at
# all, and the failure arrived as an empty password with the diagnostic
# swallowed by pipefail.
"$WORK/mailyard" serve --init --config "$WORK/mailyard.yaml" >"$WORK/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 60); do
  grep -q "password:" "$WORK/server.log" && break
  sleep 1
done
# `|| true` because pipefail turns a grep that matched nothing into a
# fatal assignment, which killed the script one line before the check
# below could say why.
PW=$(grep -o 'password: .*' "$WORK/server.log" | head -1 | sed 's/password: //' || true)
if [ -z "$PW" ]; then
  echo "audit: the server never printed a bootstrap password" >&2
  tail -20 "$WORK/server.log" >&2
  exit 1
fi

AUDIT_URL="http://localhost:$APP_PORT" \
AUDIT_ADMIN_PW="$PW" \
AUDIT_SPEC="$WORK/openapi.yaml" \
  python3 scripts/audit-permissions.py

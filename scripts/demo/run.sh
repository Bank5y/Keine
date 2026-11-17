#!/usr/bin/env bash
# A throwaway Keine with sample data, for looking at the UI: screenshots,
# PR evidence, design references. It never touches a real instance: its own
# Postgres container, its own port and a temporary data directory.
#
#   scripts/demo/run.sh        build the working tree, start, fill with sample data
#   scripts/demo/run.sh stop   stop it and delete its database and files
#
# Sample data (scripts/demo/seed.py): six public courses, one private course
# with a pending publish request; bob subscribed to "Agent 系统设计 40 讲" with
# progress and annotations; bob's update request from a draft; alice's draft
# with two conflicts. Logins: alice / bob / carol with password <name>-password,
# admin / admin-password. Editing: admin edits any course directly; alice's
# course 1 is public, so her edits there become an update request.
#
# Screenshots and browser flows: scripts/demo/shoot.py.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${DEMO_PORT:-18080}
PG_PORT=${DEMO_PG_PORT:-55432}
PG=keine-demo-pg
STATE=${DEMO_DIR:-${TMPDIR:-/tmp}/keine-demo}
export KEINE_ADMIN_USERNAME=admin KEINE_ADMIN_PASSWORD=admin-password
export DEMO_URL=http://127.0.0.1:$PORT

stop() {
  if [[ -f $STATE/keine.pid ]]; then kill "$(cat "$STATE/keine.pid")" 2>/dev/null || true; fi
  docker rm -f "$PG" >/dev/null 2>&1 || true
  rm -rf "$STATE"
}

# wait_for WHAT CMD... retries CMD for 30s; it fails the script if CMD never succeeds.
wait_for() {
  local what=$1; shift
  for _ in $(seq 1 30); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  echo "demo: $what did not come up" >&2
  return 1
}

if [[ ${1:-} == stop ]]; then stop; echo "demo stopped"; exit 0; fi

stop
mkdir -p "$STATE/data"
docker run -d --rm --name "$PG" -e POSTGRES_PASSWORD=demo -e POSTGRES_DB=keine \
  -p "127.0.0.1:$PG_PORT:5432" postgres:17-alpine >/dev/null
(cd "$ROOT/web" && npm run -s build >/dev/null)
(cd "$ROOT" && go build -o "$STATE/keine" ./cmd/keine)
# over TCP: the image's init runs a temporary server on the socket only, then
# restarts, so a socket check passes too early
wait_for postgres docker exec "$PG" pg_isready -h 127.0.0.1 -U postgres -q

KEINE_ADDR=127.0.0.1:$PORT KEINE_DATA_DIR=$STATE/data \
KEINE_DATABASE_URL="postgres://postgres:demo@127.0.0.1:$PG_PORT/keine?sslmode=disable" \
  nohup "$STATE/keine" >"$STATE/keine.log" 2>&1 &
echo $! >"$STATE/keine.pid"
wait_for keine curl -sf -o /dev/null "$DEMO_URL/" || { tail -n 20 "$STATE/keine.log" >&2; exit 1; }

python3 "$ROOT/scripts/demo/seed.py" >"$STATE/seed.json"
echo "demo at $DEMO_URL (log: $STATE/keine.log, ids: $STATE/seed.json)"
echo "stop with: scripts/demo/run.sh stop"

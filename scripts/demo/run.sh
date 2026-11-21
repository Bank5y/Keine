#!/usr/bin/env bash
# A throwaway Keine with sample data, for looking at the UI: screenshots,
# PR evidence, design references. It never touches a real instance: its own
# Postgres container, its own port and a temporary data directory.
#
#   scripts/demo/run.sh        build the working tree, start, fill with sample data
#   scripts/demo/run.sh stop   stop it and delete its database and files
#
# Several at once (one per worktree): give each its own DEMO_PORT, DEMO_PG_PORT,
# DEMO_MODEL_PORT, DEMO_PG_NAME and DEMO_DIR, and pass the same variables to stop.
#
# AI: a stand-in model endpoint (scripts/demo/fake_model.py, on DEMO_MODEL_PORT)
# is the site model "演示模型", serving every run kind, so the assistant is
# available; the admin's 模型 tab shows it. Its key "bad" fails the connection test.
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
MODEL_PORT=${DEMO_MODEL_PORT:-18999}
PG=${DEMO_PG_NAME:-keine-demo-pg}
STATE=${DEMO_DIR:-${TMPDIR:-/tmp}/keine-demo}
export KEINE_ADMIN_USERNAME=admin KEINE_ADMIN_PASSWORD=admin-password
export KEINE_MASTER_KEY=demo-master-key-not-a-secret-0123456789
export DEMO_URL=http://127.0.0.1:$PORT DEMO_MODEL_URL=http://127.0.0.1:$MODEL_PORT/v1

stop() {
  for pid in "$STATE"/keine.pid "$STATE"/model.pid; do
    if [[ -f $pid ]]; then kill "$(cat "$pid")" 2>/dev/null || true; fi
  done
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

nohup python3 "$ROOT/scripts/demo/fake_model.py" "$MODEL_PORT" >/dev/null 2>&1 &
echo $! >"$STATE/model.pid"

KEINE_ADDR=127.0.0.1:$PORT KEINE_DATA_DIR=$STATE/data \
KEINE_DATABASE_URL="postgres://postgres:demo@127.0.0.1:$PG_PORT/keine?sslmode=disable" \
  nohup "$STATE/keine" >"$STATE/keine.log" 2>&1 &
echo $! >"$STATE/keine.pid"
wait_for keine curl -sf -o /dev/null "$DEMO_URL/" || { tail -n 20 "$STATE/keine.log" >&2; exit 1; }

python3 "$ROOT/scripts/demo/seed.py" >"$STATE/seed.json"
echo "demo at $DEMO_URL (log: $STATE/keine.log, ids: $STATE/seed.json)"
echo "stop with: scripts/demo/run.sh stop"

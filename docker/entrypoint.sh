#!/bin/sh
set -eu

# The migrator and API server are separate processes, but startup retry time is
# one container-level budget. Both binaries derive their remaining time from
# this entrypoint timestamp. Always overwrite inherited values to avoid reusing
# a stale timestamp after a container restart.
export MULTICA_INTERNAL_DATABASE_STARTUP_STARTED_AT_UNIX="$(date +%s)"

migrate_pid=""

stop_migration() {
  signal="$1"
  exit_code="$2"
  trap - TERM INT
  if [ -n "$migrate_pid" ]; then
    kill "-$signal" "$migrate_pid" 2>/dev/null || true
    wait "$migrate_pid" 2>/dev/null || true
  fi
  exit "$exit_code"
}

trap 'stop_migration TERM 143' TERM
trap 'stop_migration INT 130' INT

echo "Running database migrations..."
./migrate up &
migrate_pid=$!
if wait "$migrate_pid"; then
  migrate_status=0
else
  migrate_status=$?
fi
migrate_pid=""
trap - TERM INT
if [ "$migrate_status" -ne 0 ]; then
  exit "$migrate_status"
fi

echo "Starting server..."
exec ./server

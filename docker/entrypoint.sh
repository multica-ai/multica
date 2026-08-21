#!/bin/sh
set -eu

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

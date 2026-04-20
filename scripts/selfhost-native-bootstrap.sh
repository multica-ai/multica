#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p logs

# Host Postgres does not auto-start in this environment after reboot.
pg_ctlcluster 14 main start >/dev/null 2>&1 || true

if command -v pg_isready >/dev/null 2>&1; then
  until pg_isready -h 127.0.0.1 -p 5432 >/dev/null 2>&1; do
    sleep 1
  done
fi

bash scripts/selfhost-native-stop.sh .env >/dev/null 2>&1 || true

nohup bash scripts/selfhost-native-backend.sh .env > logs/backend.log 2>&1 < /dev/null &
echo $! > logs/backend.pid
nohup bash scripts/selfhost-native-frontend.sh .env > logs/frontend.log 2>&1 < /dev/null &
echo $! > logs/frontend.pid

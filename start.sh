#!/bin/sh
set -e

git pull
/usr/local/bin/multica update
docker compose -f docker-compose.selfhost.yml up -d --build

# Check if daemon is running; start it if not
if ! /usr/local/bin/multica daemon status 2>/dev/null | grep -q "running"; then
  export MULTICA_APP_URL=http://localhost:3000
  export MULTICA_SERVER_URL=ws://localhost:8080/ws

  # /usr/local/bin/multica login
  /usr/local/bin/multica daemon start
fi

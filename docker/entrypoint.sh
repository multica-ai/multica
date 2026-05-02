#!/bin/sh
# Backend entrypoint. Run migrations on first boot only.
# On restart: use --entrypoint "./server" to skip migrations
# (they fail with "relation already exists" on existing DBs).
set -e

echo "Running database migrations..."
./migrate up

echo "Starting server..."
exec ./server

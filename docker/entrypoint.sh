#!/bin/sh
# Backend entrypoint. Run migrations on first boot only.
# On restart: use --entrypoint "./server" to skip migrations
# (they fail with "relation already exists" on existing DBs).
#
# IMPORTANT: This container's name must match the hostname used in
# REMOTE_API_URL in the frontend's Dockerfile/Dockerfile.web.rtl.
# Default is REMOTE_API_URL=http://backend:8080, so name the container
# "backend" — or change REMOTE_API_URL to match your container name.
set -e

echo "Running database migrations..."
./migrate up

echo "Starting server..."
exec ./server

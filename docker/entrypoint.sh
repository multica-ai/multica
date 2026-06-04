#!/bin/sh
set -e

# Ensure the local-storage upload dir exists and is writable by the app user.
# A named Docker volume mounted at /app/data/uploads is created root:root by
# default, so the build-time chown can't reach it. We fix ownership here as
# root, then drop privileges via su-exec for the actual workload.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data/uploads
    chown -R app:app /app/data
    DROP="su-exec app:app"
else
    DROP=""
fi

echo "Running database migrations..."
$DROP ./migrate up

echo "Starting server..."
exec $DROP ./server

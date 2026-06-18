# Shared local development env derivation. Source this after loading .env.

# shellcheck disable=SC1091
. scripts/selfhost-env.sh

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

export POSTGRES_DB POSTGRES_USER POSTGRES_PORT
export PORT FRONTEND_PORT FRONTEND_ORIGIN
export MULTICA_APP_URL GOOGLE_REDIRECT_URI MULTICA_SERVER_URL LOCAL_UPLOAD_BASE_URL
export PLAYWRIGHT_BASE_URL

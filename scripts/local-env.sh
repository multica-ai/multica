# Shared local development env derivation. Source this after loading .env.

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

PORT="${BACKEND_PORT:-${API_PORT:-${SERVER_PORT:-${PORT:-8080}}}}"
# `next dev --port <frontend>` overwrites process.env.PORT with the frontend
# port inside the web process, which would poison the API-proxy target in
# apps/web/config/runtime-urls.ts (it falls back to PORT). Pin BACKEND_PORT to
# the resolved backend port — the resolver prefers it over PORT and Next never
# touches it — so the same-origin /api proxy resolves correctly.
BACKEND_PORT="${PORT}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
FRONTEND_ORIGIN="${FRONTEND_ORIGIN:-http://localhost:${FRONTEND_PORT}}"

MULTICA_APP_URL="${MULTICA_APP_URL:-${FRONTEND_ORIGIN}}"
GOOGLE_REDIRECT_URI="${GOOGLE_REDIRECT_URI:-${FRONTEND_ORIGIN}/auth/callback}"
MULTICA_SERVER_URL="${MULTICA_SERVER_URL:-ws://localhost:${PORT}/ws}"
LOCAL_UPLOAD_BASE_URL="${LOCAL_UPLOAD_BASE_URL:-http://localhost:${PORT}}"
PLAYWRIGHT_BASE_URL="${PLAYWRIGHT_BASE_URL:-${FRONTEND_ORIGIN}}"

export POSTGRES_DB POSTGRES_USER POSTGRES_PORT
export PORT BACKEND_PORT FRONTEND_PORT FRONTEND_ORIGIN
export MULTICA_APP_URL GOOGLE_REDIRECT_URI MULTICA_SERVER_URL LOCAL_UPLOAD_BASE_URL
export PLAYWRIGHT_BASE_URL

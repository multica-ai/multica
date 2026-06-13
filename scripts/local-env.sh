# Shared local development env derivation. Source this after loading .env.

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

PORT="${BACKEND_PORT:-${API_PORT:-${SERVER_PORT:-${PORT:-8080}}}}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
FRONTEND_ORIGIN="${FRONTEND_ORIGIN:-http://localhost:${FRONTEND_PORT}}"

MULTICA_APP_URL="${MULTICA_APP_URL:-${FRONTEND_ORIGIN}}"
GOOGLE_REDIRECT_URI="${GOOGLE_REDIRECT_URI:-${FRONTEND_ORIGIN}/auth/callback}"
MULTICA_SERVER_URL="${MULTICA_SERVER_URL:-ws://localhost:${PORT}/ws}"
LOCAL_UPLOAD_BASE_URL="${LOCAL_UPLOAD_BASE_URL:-http://localhost:${PORT}}"
PLAYWRIGHT_BASE_URL="${PLAYWRIGHT_BASE_URL:-${FRONTEND_ORIGIN}}"

# Backend URL the Next.js dev server proxies /api, /auth, /ws, /uploads to
# (see apps/web/next.config.ts → resolveRemoteApiUrl). Without this, dev.sh leaves
# NEXT_PUBLIC_API_URL empty and resolveRemoteApiUrl falls back to $PORT — which
# `next dev` overwrites with the FRONTEND port (3000), making the proxy target the
# web server itself (self-loop → "socket hang up" on every /api request). The
# Makefile sets this for `make start`; mirror it here so `make dev` matches.
# Only a default: an explicit NEXT_PUBLIC_API_URL or REMOTE_API_URL still wins.
NEXT_PUBLIC_API_URL="${NEXT_PUBLIC_API_URL:-http://localhost:${PORT}}"
NEXT_PUBLIC_WS_URL="${NEXT_PUBLIC_WS_URL:-ws://localhost:${PORT}/ws}"

export POSTGRES_DB POSTGRES_USER POSTGRES_PORT
export PORT FRONTEND_PORT FRONTEND_ORIGIN
export MULTICA_APP_URL GOOGLE_REDIRECT_URI MULTICA_SERVER_URL LOCAL_UPLOAD_BASE_URL
export PLAYWRIGHT_BASE_URL
export NEXT_PUBLIC_API_URL NEXT_PUBLIC_WS_URL

#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env.worktree}"

if [ -f "$ENV_FILE" ] && [ "${FORCE:-0}" != "1" ]; then
  echo "Refusing to overwrite existing $ENV_FILE. Re-run with FORCE=1 if you want to regenerate it."
  exit 1
fi

worktree_name="${WORKTREE_NAME:-$(basename "$PWD")}"
slug="$(printf '%s' "$worktree_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
if [ -z "$slug" ]; then
  slug="multica"
fi

hash_value="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
offset=$((hash_value % 1000))

postgres_db="multica_${slug}_${offset}"
postgres_port=5432
backend_port=$((18080 + offset))
frontend_origin="${FRONTEND_ORIGIN:-http://localhost:3000}"

cat > "$ENV_FILE" <<EOF
POSTGRES_DB=${postgres_db}
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=${postgres_port}
DATABASE_URL=postgres://multica:multica@localhost:${postgres_port}/${postgres_db}?sslmode=disable

PORT=${backend_port}
JWT_SECRET=change-me-in-production
MULTICA_DEV_VERIFICATION_CODE=888888
MULTICA_SERVER_URL=ws://localhost:${backend_port}/ws
MULTICA_APP_URL=${frontend_origin}

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GOOGLE_REDIRECT_URI=${frontend_origin}/auth/callback

FRONTEND_ORIGIN=${frontend_origin}
EOF

echo "Generated $ENV_FILE for worktree '$worktree_name'"
echo "  Shared Postgres: localhost:${postgres_port}"
echo "  Database: ${postgres_db}"
echo "  Backend:  http://localhost:${backend_port}"
echo "  Desktop:  pnpm dev:desktop"
echo ""
echo "Next steps:"
echo "  make setup-worktree"
echo "  make start-worktree"

#!/usr/bin/env bash
# Replace the current checkout's local Docker database with a snapshot of the
# home Kubernetes Postgres. Does not touch the cluster. Refuses to run when
# DATABASE_URL is not loopback.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

ENV_FILE="${1:-.env}"
NAMESPACE="${MULTICA_K8S_NAMESPACE:-multica}"
POSTGRES_DEPLOY="${MULTICA_K8S_POSTGRES_DEPLOY:-multica-postgres}"
CONFIGMAP="${MULTICA_K8S_CONFIGMAP:-multica-config}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE" >&2
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree." >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required to snapshot the home cluster database." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
# shellcheck disable=SC1091
. scripts/local-env.sh

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
DATABASE_URL="${DATABASE_URL:-}"

case "$DATABASE_URL" in
  "" | *@localhost:* | *@localhost/* | *@127.0.0.1:* | *@127.0.0.1/* | *@\[::1\]:* | *@\[::1\]/*) ;;
  *)
    echo "Refusing to overwrite: DATABASE_URL points at a remote host." >&2
    exit 1
    ;;
esac

case "$POSTGRES_DB" in
  postgres | template0 | template1)
    echo "Refusing to overwrite protected PostgreSQL database '$POSTGRES_DB'." >&2
    exit 1
    ;;
esac

context="$(kubectl config current-context 2>/dev/null || true)"
src_db="$(kubectl -n "$NAMESPACE" get configmap "$CONFIGMAP" -o jsonpath='{.data.POSTGRES_DB}' 2>/dev/null || true)"
src_user="$(kubectl -n "$NAMESPACE" get configmap "$CONFIGMAP" -o jsonpath='{.data.POSTGRES_USER}' 2>/dev/null || true)"
src_db="${src_db:-multica}"
src_user="${src_user:-multica}"

echo "==> Snapshot ${NAMESPACE}/${POSTGRES_DEPLOY}  (context: ${context:-unknown})"
echo "    source: ${src_user}@${src_db}"
echo "    local:  ${POSTGRES_USER}@${POSTGRES_DB}  (${ENV_FILE})"

kubectl -n "$NAMESPACE" exec "deploy/${POSTGRES_DEPLOY}" -c postgres -- \
  pg_isready -U "$src_user" -d "$src_db" >/dev/null

dump="$(mktemp "${TMPDIR:-/tmp}/multica-k8s-pg-XXXXXX.dump")"
cleanup() { rm -f "$dump"; }
trap cleanup EXIT

echo "==> Dumping cluster database..."
kubectl -n "$NAMESPACE" exec "deploy/${POSTGRES_DEPLOY}" -c postgres -- \
  pg_dump -U "$src_user" -d "$src_db" --no-owner --no-acl --format=custom \
  >"$dump"

bash scripts/ensure-postgres.sh "$ENV_FILE"

echo "==> Replacing local database '${POSTGRES_DB}'..."
docker compose exec -T postgres psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS \"${POSTGRES_DB}\" WITH (FORCE);" \
  -c "CREATE DATABASE \"${POSTGRES_DB}\";"

echo "==> Restoring snapshot..."
docker compose exec -T postgres pg_restore \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --no-owner \
  --no-acl \
  --exit-on-error \
  <"$dump"

echo "==> Applying local migrations (no-op if the snapshot is current)..."
(cd server && go run ./cmd/migrate up)

echo ""
echo "✓ Local database '${POSTGRES_DB}' now matches ${NAMESPACE} (${context:-cluster})."
echo "  Log in again on http://localhost:${FRONTEND_PORT:-3000}; session cookies from the cluster site do not carry over."

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

stub_bin="$tmp_dir/bin"
mkdir -p "$stub_bin"

cat >"$tmp_dir/test.env" <<'ENV'
POSTGRES_DB=multica_test_worktree
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=5432
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_test_worktree?sslmode=disable
ENV

cat >"$stub_bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TEST_DOCKER_LOG"
exit 0
STUB

cat >"$stub_bin/pg_isready" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TEST_PSQL_LOG"
exit 0
STUB

cat >"$stub_bin/psql" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TEST_PSQL_LOG"
exit 0
STUB

chmod +x "$stub_bin/docker" "$stub_bin/pg_isready" "$stub_bin/psql"

TEST_DOCKER_LOG="$tmp_dir/docker.log" \
TEST_PSQL_LOG="$tmp_dir/psql.log" \
PATH="$stub_bin:/usr/bin:/bin" \
  bash "$ROOT_DIR/scripts/ensure-postgres.sh" "$tmp_dir/test.env" >/dev/null

if grep -q '^compose exec ' "$tmp_dir/docker.log"; then
  echo "database checks must use the same host connection as DATABASE_URL, not docker exec" >&2
  exit 1
fi

if ! grep -q -- '-h localhost -p 5432 -U multica -d postgres' "$tmp_dir/psql.log"; then
  echo "database checks did not use the DATABASE_URL host and port" >&2
  cat "$tmp_dir/psql.log" >&2
  exit 1
fi

echo "PostgreSQL worktree connection routing ok"

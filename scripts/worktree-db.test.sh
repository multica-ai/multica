#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require_contains() {
  local file=$1
  local expected=$2
  if ! grep -Fq "$expected" "$file"; then
    echo "Expected output to contain: $expected" >&2
    echo "Observed:" >&2
    cat "$file" >&2
    exit 1
  fi
}

stub_dir="$tmp_dir/bin"
mkdir -p "$stub_dir"
docker_log="$tmp_dir/docker.log"

cat >"$stub_dir/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$DOCKER_LOG"
STUB
chmod +x "$stub_dir/docker"

local_env="$tmp_dir/local.env"
cat >"$local_env" <<'EOF'
POSTGRES_DB=multica_feature_123
POSTGRES_USER=multica
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_feature_123?sslmode=disable
EOF

output="$tmp_dir/output"
: >"$docker_log"
cancel_status=0
printf 'n\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$local_env" >"$output" 2>&1 || cancel_status=$?
if [ "$cancel_status" -ne 2 ]; then
  fail "db-drop cancellation status = $cancel_status, want 2"
fi
require_contains "$output" "Drop this database permanently? [y/N] Cancelled."
if [ -s "$docker_log" ]; then
  fail "db-drop invoked Docker after confirmation was declined"
fi

if ! printf 'n\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  make --no-print-directory -C "$root_dir" db-drop ENV_FILE="$local_env" >"$output" 2>&1; then
  fail "make db-drop must treat an intentional cancellation as success"
fi
require_contains "$output" "Cancelled."
if grep -Fq "Error" "$output"; then
  fail "make db-drop printed a Make error after cancellation"
fi

: >"$docker_log"
printf 'y\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$local_env" >"$output"
require_contains "$docker_log" \
  "compose exec -T postgres dropdb --username multica --maintenance-db postgres --if-exists --force -- multica_feature_123"
require_contains "$output" "Dropped database 'multica_feature_123'."

remote_env="$tmp_dir/remote.env"
cat >"$remote_env" <<'EOF'
POSTGRES_DB=production
DATABASE_URL=postgres://user:password@db.example.com:5432/production
EOF
: >"$docker_log"
if printf 'y\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$remote_env" >"$output" 2>&1; then
  fail "db-drop must refuse a remote DATABASE_URL"
fi
require_contains "$output" "DATABASE_URL points at a remote host"
if [ -s "$docker_log" ]; then
  fail "db-drop invoked Docker for a remote DATABASE_URL"
fi

system_env="$tmp_dir/system.env"
cat >"$system_env" <<'EOF'
POSTGRES_DB=postgres
DATABASE_URL=postgres://multica:multica@localhost:5432/postgres
EOF
if printf 'y\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$system_env" >"$output" 2>&1; then
  fail "db-drop must protect PostgreSQL system databases"
fi
require_contains "$output" "Refusing to drop protected PostgreSQL database"

main_env="$tmp_dir/main.env"
cat >"$main_env" <<'EOF'
POSTGRES_DB=multica
DATABASE_URL=postgres://multica:multica@localhost:5432/multica
EOF
if printf 'y\n' | PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$main_env" >"$output" 2>&1; then
  fail "db-drop must protect the default main database"
fi
require_contains "$output" "Refusing to drop the default main database"

: >"$docker_log"
cancel_status=0
PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/drop-database.sh" "$local_env" </dev/null >"$output" 2>&1 || cancel_status=$?
if [ "$cancel_status" -ne 2 ]; then
  fail "db-drop EOF cancellation status = $cancel_status, want 2"
fi
require_contains "$output" "Cancelled."
if [ -s "$docker_log" ]; then
  fail "db-drop invoked Docker without affirmative confirmation"
fi

repo="$tmp_dir/repo"
worktree="$tmp_dir/worktree"
git init -q -b main "$repo"
git -C "$repo" config user.name "Worktree DB Test"
git -C "$repo" config user.email "worktree-db-test@example.com"
printf '.env.worktree\n' >"$repo/.gitignore"
printf 'base\n' >"$repo/tracked.txt"
mkdir -p "$repo/backend"
printf 'nested\n' >"$repo/backend/tracked.txt"
git -C "$repo" add .gitignore tracked.txt backend/tracked.txt
git -C "$repo" commit -qm "test: initialize fixture"
git -C "$repo" worktree add -q -b feature "$worktree"
cat >"$worktree/.env.worktree" <<'EOF'
POSTGRES_DB=multica_worktree_456
POSTGRES_USER=multica
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_worktree_456?sslmode=disable
EOF

: >"$docker_log"
if printf 'y\n' | (cd "$worktree/backend" && PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/remove-worktree.sh" "$worktree") >"$output" 2>&1; then
  fail "remove-worktree must refuse the current worktree from a nested directory"
fi
require_contains "$output" "Refusing to remove the current worktree from inside itself."
if [ ! -d "$worktree" ]; then
  fail "remove-worktree removed the current worktree from a nested directory"
fi
if [ -s "$docker_log" ]; then
  fail "remove-worktree dropped the database before rejecting the current worktree"
fi

: >"$docker_log"
if ! printf 'n\n' | (cd "$repo" && PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/remove-worktree.sh" "$worktree") >"$output" 2>&1; then
  fail "remove-worktree must treat an intentional cancellation as success"
fi
if [ ! -d "$worktree" ]; then
  fail "remove-worktree removed the worktree after database deletion was declined"
fi
require_contains "$output" "Worktree was not removed."
if grep -Fq "Error" "$output"; then
  fail "remove-worktree printed an error after cancellation"
fi

printf 'y\n' | (cd "$repo" && PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/remove-worktree.sh" "$worktree") >"$output"
if [ -e "$worktree" ]; then
  fail "remove-worktree did not remove the worktree after database deletion"
fi
require_contains "$docker_log" \
  "compose exec -T postgres dropdb --username multica --maintenance-db postgres --if-exists --force -- multica_worktree_456"

dirty_worktree="$tmp_dir/dirty-worktree"
git -C "$repo" worktree add -q -b dirty-feature "$dirty_worktree"
printf 'dirty\n' >>"$dirty_worktree/tracked.txt"
: >"$docker_log"
if printf 'y\n' | (cd "$repo" && PATH="$stub_dir:$PATH" DOCKER_LOG="$docker_log" \
  bash "$root_dir/scripts/remove-worktree.sh" "$dirty_worktree") >"$output" 2>&1; then
  fail "remove-worktree must refuse a dirty worktree"
fi
require_contains "$output" "Refusing to remove dirty worktree"
if [ -s "$docker_log" ]; then
  fail "remove-worktree dropped the database before rejecting a dirty worktree"
fi

empty_worktree="$tmp_dir/empty-worktree"
git -C "$repo" worktree add -q -b empty-feature "$empty_worktree"
(cd "$repo" && bash "$root_dir/scripts/remove-worktree.sh" "$empty_worktree") >"$output"
require_contains "$output" "No .env.worktree found; skipping database cleanup."
if [ -e "$empty_worktree" ]; then
  fail "remove-worktree did not remove a worktree without an env file"
fi

if (cd "$repo" && bash "$root_dir/scripts/remove-worktree.sh" "$repo") >"$output" 2>&1; then
  fail "remove-worktree must refuse the primary checkout"
fi
require_contains "$output" "Refusing to remove the primary checkout"

echo "worktree database cleanup tests passed"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/selfhost-env.sh"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

assert_line() {
  local file="$1"
  local expected="$2"
  if ! grep -Fxq "$expected" "$file"; then
    echo "expected line not found: $expected" >&2
    echo "--- $file ---" >&2
    cat "$file" >&2
    exit 1
  fi
}

example_env="$tmp_dir/example.env"
cp "$ROOT_DIR/.env.example" "$example_env"

jwt="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
postgres_password="abcdef0123456789abcdef0123456789abcdef0123456789"

bash "$SCRIPT" "$example_env" "$jwt" "$postgres_password"

assert_line "$example_env" "JWT_SECRET=$jwt"
assert_line "$example_env" "POSTGRES_PASSWORD=$postgres_password"
assert_line "$example_env" "DATABASE_URL=postgres://multica:$postgres_password@localhost:5432/multica?sslmode=disable"

custom_env="$tmp_dir/custom.env"
cat > "$custom_env" <<'EOF'
POSTGRES_USER=app_user
POSTGRES_PASSWORD=old-password
POSTGRES_DB=prod_db
DATABASE_URL=postgresql://app_user:old-password@db.internal:6543/prod_db?sslmode=require
JWT_SECRET=change-me
EOF

bash "$SCRIPT" "$custom_env" "$jwt" "$postgres_password"

assert_line "$custom_env" "POSTGRES_PASSWORD=$postgres_password"
assert_line "$custom_env" "DATABASE_URL=postgresql://app_user:$postgres_password@db.internal:6543/prod_db?sslmode=require"

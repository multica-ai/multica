#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_output() {
  local actual=$1
  local expected=$2

  if [ "$actual" != "$expected" ]; then
    echo "Unexpected self-host URL helper output:"
    echo "  expected: $expected"
    echo "  actual:   $actual"
    exit 1
  fi
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

default_env="$tmp_dir/default.env"
cat >"$default_env" <<'ENV'
BIND_HOST=127.0.0.1
FRONTEND_PORT=3100
BACKEND_PORT=9100
ENV

require_output "$(bash scripts/selfhost-url.sh "$default_env" frontend)" "http://localhost:3100"
require_output "$(bash scripts/selfhost-url.sh "$default_env" backend)" "http://localhost:9100"
require_output "$(bash scripts/selfhost-url.sh "$default_env" health)" "http://localhost:9100/health"

lan_env="$tmp_dir/lan.env"
cat >"$lan_env" <<'ENV'
BIND_HOST=192.168.1.50
FRONTEND_PORT=3100
BACKEND_PORT=9100
ENV

require_output "$(bash scripts/selfhost-url.sh "$lan_env" frontend)" "http://192.168.1.50:3100"
require_output "$(bash scripts/selfhost-url.sh "$lan_env" backend)" "http://192.168.1.50:9100"
require_output "$(bash scripts/selfhost-url.sh "$lan_env" health)" "http://192.168.1.50:9100/health"

all_interface_env="$tmp_dir/all-interface.env"
cat >"$all_interface_env" <<'ENV'
BIND_HOST=0.0.0.0
FRONTEND_PORT=3100
BACKEND_PORT=9100
ENV

require_output "$(bash scripts/selfhost-url.sh "$all_interface_env" health)" "http://localhost:9100/health"

ipv6_env="$tmp_dir/ipv6.env"
cat >"$ipv6_env" <<'ENV'
BIND_HOST=::1
FRONTEND_PORT=3100
BACKEND_PORT=9100
ENV

require_output "$(bash scripts/selfhost-url.sh "$ipv6_env" health)" "http://[::1]:9100/health"

echo "self-host URL helper tests passed"

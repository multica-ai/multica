#!/usr/bin/env bash
set -euo pipefail

source scripts/check-db-pool.sh

unset DATABASE_MAX_CONNS DATABASE_MIN_CONNS
configure_check_db_pool

test "$DATABASE_MAX_CONNS" = "4"
test "$DATABASE_MIN_CONNS" = "1"

DATABASE_MAX_CONNS=9
DATABASE_MIN_CONNS=3
configure_check_db_pool

test "$DATABASE_MAX_CONNS" = "9"
test "$DATABASE_MIN_CONNS" = "3"

echo "check database pool tests passed"

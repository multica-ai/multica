#!/usr/bin/env bash

# Full local checks use one Playwright worker, so they do not need the
# production-sized database pool. Keeping this pool small prevents concurrent
# worktree checks on a shared local PostgreSQL server from exhausting its
# connection limit. Explicit operator overrides still win.
configure_check_db_pool() {
  : "${DATABASE_MAX_CONNS:=4}"
  : "${DATABASE_MIN_CONNS:=1}"
  export DATABASE_MAX_CONNS DATABASE_MIN_CONNS
}

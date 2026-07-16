#!/usr/bin/env bash

# pnpm/turbo keep child processes alive after their immediate parent receives
# SIGTERM. Stop descendants first so the local check cannot hang in cleanup.
stop_check_process_tree() {
  local root_pid=$1 child_pid
  while read -r child_pid; do
    [ -n "$child_pid" ] && stop_check_process_tree "$child_pid"
  done < <(pgrep -P "$root_pid" 2>/dev/null || true)
  kill "$root_pid" 2>/dev/null || true
  wait "$root_pid" 2>/dev/null || true
}

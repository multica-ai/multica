#!/usr/bin/env bash
# Shared agent issue label + local dispatch helpers for hands-off scripts.
# shellcheck shell=bash

DISPATCH_LEASE_SECONDS="${DISPATCH_LEASE_SECONDS:-7200}"
DISPATCH_CLEANUP_MIN_AGE_SEC="${DISPATCH_CLEANUP_MIN_AGE_SEC:-120}"

# macOS has no flock(1); mkdir lock dir is portable.
_SINGLETON_LOCK_DIR=""

_release_singleton_lock() {
  if [ -n "${_SINGLETON_LOCK_DIR:-}" ]; then
    rm -rf "$_SINGLETON_LOCK_DIR"
    _SINGLETON_LOCK_DIR=""
  fi
}

acquire_singleton_lock() {
  local name="${1:?}" state_dir="${2:?}" stale_sec="${3:-7200}"
  local lock="${state_dir}/${name}.lock.d" now mtime age
  if [ -d "$lock" ]; then
    now="$(date +%s)"
    mtime="$(stat -f %m "$lock" 2>/dev/null || stat -c %Y "$lock" 2>/dev/null || echo 0)"
    age=$((now - mtime))
    if [ "$age" -gt "$stale_sec" ]; then
      rm -rf "$lock"
    fi
  fi
  if mkdir "$lock" 2>/dev/null; then
    _SINGLETON_LOCK_DIR="$lock"
    printf '%s\n' "$$" >"$lock/pid"
    trap '_release_singleton_lock' EXIT
    return 0
  fi
  return 1
}

_dispatch_lock_path() {
  local repo_root="${1:?}" num="${2:?}"
  echo "$repo_root/.delivery/.agent-runs/.dispatch-issue-${num}.lock"
}

_dispatch_lock_expired() {
  local lock_file="${1:?}"
  [ ! -f "$lock_file" ] && return 0
  if _dispatch_lock_alive "$lock_file"; then
    return 1
  fi
  local pid started expiry now
  pid="$(sed -n '1p' "$lock_file" 2>/dev/null || true)"
  started="$(sed -n '2p' "$lock_file" 2>/dev/null || true)"
  expiry="$(sed -n '3p' "$lock_file" 2>/dev/null || true)"
  now="$(date +%s)"
  if [ -n "$expiry" ] && [ "$expiry" -lt "$now" ]; then
    return 0
  fi
  if [ -n "$started" ] && [ -z "$expiry" ]; then
    if [ $((now - started)) -gt "$DISPATCH_LEASE_SECONDS" ]; then
      return 0
    fi
  fi
  if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi
  return 1
}

write_dispatch_lock() {
  local repo_root="${1:?}" num="${2:?}" pid="${3:?}" agent_pid="${4:-}"
  local lock_file
  lock_file="$(_dispatch_lock_path "$repo_root" "$num")"
  mkdir -p "$(dirname "$lock_file")"
  local now expiry
  now="$(date +%s)"
  expiry=$((now + DISPATCH_LEASE_SECONDS))
  if [ -n "$agent_pid" ]; then
    printf '%s\n%s\n%s\n%s\n' "$pid" "$now" "$expiry" "$agent_pid" >"$lock_file"
  else
    printf '%s\n%s\n%s\n' "$pid" "$now" "$expiry" >"$lock_file"
  fi
}

clear_dispatch_lock() {
  local repo_root="${1:?}" num="${2:?}"
  rm -f "$(_dispatch_lock_path "$repo_root" "$num")"
}

record_dispatch_agent_pid() {
  local repo_root="${1:?}" num="${2:?}" agent_pid="${3:?}"
  local lock_file dispatch_pid started expiry
  lock_file="$(_dispatch_lock_path "$repo_root" "$num")"
  [ -f "$lock_file" ] || return 0
  dispatch_pid="$(sed -n '1p' "$lock_file" 2>/dev/null || true)"
  started="$(sed -n '2p' "$lock_file" 2>/dev/null || true)"
  expiry="$(sed -n '3p' "$lock_file" 2>/dev/null || true)"
  printf '%s\n%s\n%s\n%s\n' "$dispatch_pid" "$started" "$expiry" "$agent_pid" >"$lock_file"
}

# cursor-agent CLI often appears as: cursor-agent --use-system-ca .../index.js -p --worktree cursor-issue-N
# (not "cursor-agent -p", so naive pgrep patterns false-negative and cleanup kills live dispatches).
_pgrep_portfolio_agent_for_issue() {
  local issue="${1:?}"
  pgrep -fl "dispatch-cursor-agent-cli\\.sh[[:space:]]+${issue}( |$)" >/dev/null 2>&1 && return 0
  pgrep -fl "cursor-issue-${issue}" >/dev/null 2>&1 && return 0
  pgrep -fl "[[:space:]]-p[[:space:]].*cursor-issue-${issue}" >/dev/null 2>&1 && return 0
  return 1
}

_pgrep_any_portfolio_agent() {
  pgrep -fl 'dispatch-cursor-agent-cli\.sh' >/dev/null 2>&1 && return 0
  pgrep -fl 'cursor-issue-[0-9]+' >/dev/null 2>&1 && return 0
  pgrep -fl '[[:space:]]-p[[:space:]].*--worktree cursor-issue-' >/dev/null 2>&1 && return 0
  return 1
}

_dispatch_lock_issue() {
  local lock_file="${1:?}"
  local base
  base="$(basename "$lock_file")"
  if [[ "$base" =~ ^\.dispatch-issue-([0-9]+)\.lock$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

_dispatch_lock_alive() {
  local lock_file="${1:?}"
  [ -f "$lock_file" ] || return 1
  local pid issue
  pid="$(sed -n '1p' "$lock_file" 2>/dev/null || true)"
  issue="$(_dispatch_lock_issue "$lock_file" 2>/dev/null || true)"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    return 0
  fi
  agent_pid="$(sed -n '4p' "$lock_file" 2>/dev/null || true)"
  if [ -n "$agent_pid" ] && kill -0 "$agent_pid" 2>/dev/null; then
    return 0
  fi
  if [ -n "$issue" ] && _pgrep_portfolio_agent_for_issue "$issue"; then
    return 0
  fi
  return 1
}

local_dispatch_running_count() {
  local n=0
  local line issue seen=""
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    if [[ "$line" == *"/bin/zsh"* ]] || [[ "$line" == *" zsh -c"* ]]; then
      continue
    fi
    if [[ "$line" =~ dispatch-cursor-agent-cli\.sh[[:space:]]+([0-9]+) ]]; then
      issue="${BASH_REMATCH[1]}"
      case "$seen" in *" $issue "*) continue ;; esac
      seen="$seen $issue "
      n=$((n + 1))
      continue
    fi
    if [[ "$line" =~ cursor-issue-([0-9]+) ]]; then
      issue="${BASH_REMATCH[1]}"
      case "$seen" in *" $issue "*) continue ;; esac
      seen="$seen $issue "
      n=$((n + 1))
    fi
  done < <(pgrep -fl 'dispatch-cursor-agent-cli\.sh|cursor-issue-[0-9]+' 2>/dev/null || true)
  echo "${n:-0}"
}

cleanup_stale_local_dispatches() {
  local quiet="${1:-0}"
  local killed=0
  local line pid issue repo repo_root state

  while IFS= read -r line; do
    [ -z "$line" ] && continue
    if [[ ! "$line" =~ dispatch-cursor-agent-cli\.sh[[:space:]]+([0-9]+) ]]; then
      continue
    fi
    pid="${line%% *}"
    issue="${BASH_REMATCH[1]}"
    repo_root=""
    if [[ "$line" =~ REPO_ROOT=([^[:space:]]+) ]]; then
      repo_root="${BASH_REMATCH[1]}"
    fi
    repo=""
    if [ -z "$repo_root" ]; then
      repo_root="$(lsof -p "$pid" 2>/dev/null | awk '/cwd/ {print $NF; exit}' || true)"
    fi
    if [ -n "$repo_root" ] && [ -d "$repo_root" ]; then
      repo="$(gh repo view "$repo_root" --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
    fi
    if [ -z "$repo" ]; then
      continue
    fi
    state="$(gh issue view "$issue" -R "$repo" --json state -q .state 2>/dev/null || echo OPEN)"
    if [ "$state" = "CLOSED" ]; then
      if [ "$quiet" -eq 0 ]; then
        echo "cleanup: kill stale dispatch pid=$pid ($repo#$issue closed)"
      fi
      kill "$pid" 2>/dev/null || true
      killed=$((killed + 1))
      continue
    fi
    if ! pgrep -P "$pid" >/dev/null 2>&1 && ! _pgrep_portfolio_agent_for_issue "$issue"; then
      lock="${repo_root}/.delivery/.agent-runs/.dispatch-issue-${issue}.lock"
      if [ -f "$lock" ]; then
        lock_pid="$(sed -n '1p' "$lock" 2>/dev/null || true)"
        lock_started="$(sed -n '2p' "$lock" 2>/dev/null || true)"
        lock_age=999999
        if [ -n "$lock_started" ]; then
          lock_age=$(( $(date +%s) - lock_started ))
        fi
        if [ "$lock_pid" = "$pid" ] && [ "$lock_age" -ge "$DISPATCH_CLEANUP_MIN_AGE_SEC" ] && ! _pgrep_any_portfolio_agent; then
          if [ "$quiet" -eq 0 ]; then
            echo "cleanup: kill stuck dispatch pid=$pid ($repo#$issue no agent child)"
          fi
          kill "$pid" 2>/dev/null || true
          rm -f "$lock"
          killed=$((killed + 1))
        fi
      fi
    fi
  done < <(pgrep -fl 'dispatch-cursor-agent-cli\.sh' 2>/dev/null || true)

  # Zsh wrappers left after manual test runs.
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    if [[ "$line" == *"dispatch-cursor-agent-cli.sh"* ]]; then
      pid="${line%% *}"
      if [ "$quiet" -eq 0 ]; then
        echo "cleanup: kill wrapper pid=$pid"
      fi
      kill "$pid" 2>/dev/null || true
      killed=$((killed + 1))
    fi
  done < <(pgrep -fl '/bin/zsh.*dispatch-cursor-agent-cli\.sh' 2>/dev/null || true)

  # Stale lock files (dead pid or expired lease).
  while IFS= read -r lock; do
    [ -f "$lock" ] || continue
    if _dispatch_lock_alive "$lock"; then
      continue
    fi
    if _dispatch_lock_expired "$lock"; then
      if [ "$quiet" -eq 0 ]; then
        echo "cleanup: remove expired lock $lock"
      fi
      rm -f "$lock"
    fi
  done < <(find "$HOME/Projects" -path '*/.delivery/.agent-runs/.dispatch-issue-*.lock' 2>/dev/null || true)

  echo "$killed"
}

reconcile_stale_running_labels() {
  local repo="${1:?}" repo_root="${2:-}" dry="${3:-0}"
  local num labels quiet=0
  [ "$dry" -eq 1 ] && quiet=1
  numbers="$(gh issue list -R "$repo" -s open -l agent-running --json number -q '.[].number' 2>/dev/null || true)"
  for num in $numbers; do
    [ -z "$num" ] && continue
    if issue_dispatch_active "$num" "$repo_root"; then
      continue
    fi
    if [ "$quiet" -eq 0 ]; then
      echo "reconcile-running: $repo#$num → agent-safe (no live dispatch)"
    else
      echo "would re-queue running: $repo#$num"
    fi
    [ "$dry" -eq 1 ] && continue
    gh issue edit "$num" -R "$repo" --remove-label "agent-running" 2>/dev/null || true
    gh issue edit "$num" -R "$repo" --add-label "agent-safe" 2>/dev/null || true
    [ -n "$repo_root" ] && clear_dispatch_lock "$repo_root" "$num" 2>/dev/null || true
  done
}

_blocked_retryable() {
  local body="${1:-}" log_path=""
  if [[ "$body" == *"Authentication required"* ]] || [[ "$body" == *CURSOR_API_KEY* ]] || [[ "$body" == *"agent login"* ]]; then
    return 0
  fi
  if [[ "$body" == *"假死清理"* ]] || [[ "$body" == *"dispatch pid 已死"* ]] || [[ "$body" == *"dispatch pid 已不存在"* ]]; then
    return 0
  fi
  if [[ "$body" == *"Local cursor-agent failed"* ]] && [[ "$body" =~ \`([^\`]+\.log)\` ]]; then
    log_path="${BASH_REMATCH[1]}"
    if [ -f "$log_path" ]; then
      if grep -qE 'Authentication required|Please run .agent login|CURSOR_API_KEY' "$log_path" 2>/dev/null; then
        return 0
      fi
      if grep -q 'starting cursor-agent' "$log_path" 2>/dev/null && ! grep -qE 'completed|exit code|PR opened' "$log_path" 2>/dev/null; then
        return 0
      fi
    fi
  fi
  if [[ "$body" =~ \`([^\`]+\.log)\` ]]; then
    log_path="${BASH_REMATCH[1]}"
    if [ -f "$log_path" ] && grep -qE 'Authentication required|Please run .agent login|CURSOR_API_KEY' "$log_path" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
}

reconcile_auth_blocked_retries() {
  local repo="${1:?}" dry="${2:-0}"
  local bin="${CURSOR_AGENT_BIN:-cursor-agent}" num body quiet=0 reason=""
  [ "$dry" -eq 1 ] && quiet=1
  command -v "$bin" >/dev/null 2>&1 || return 0
  "$bin" status >/dev/null 2>&1 || return 0
  numbers="$(gh issue list -R "$repo" -s open -l agent-blocked --json number -q '.[].number' 2>/dev/null || true)"
  for num in $numbers; do
    [ -z "$num" ] && continue
    body="$(gh issue view "$num" -R "$repo" --json comments -q '.comments[-1].body // ""' 2>/dev/null || true)"
    if ! _blocked_retryable "$body"; then
      continue
    fi
    if [[ "$body" == *"假死清理"* ]] || [[ "$body" == *"dispatch pid"* ]]; then
      reason="zombie retry"
    else
      reason="auth retry"
    fi
    if [ "$quiet" -eq 0 ]; then
      echo "reconcile-blocked: $repo#$num → agent-safe ($reason)"
    else
      echo "would blocked-retry ($reason): $repo#$num"
    fi
    [ "$dry" -eq 1 ] && continue
    gh issue edit "$num" -R "$repo" --remove-label "agent-blocked" --add-label "agent-safe" 2>/dev/null || true
  done
}

issue_dispatch_active() {
  local num="${1:?}"
  local repo_root="${2:-}"
  local lock_file=""
  if [ -n "$repo_root" ]; then
    lock_file="$(_dispatch_lock_path "$repo_root" "$num")"
    if [ -f "$lock_file" ]; then
      if _dispatch_lock_expired "$lock_file"; then
        rm -f "$lock_file"
      else
        return 0
      fi
    fi
  fi
  if pgrep -fl "dispatch-cursor-agent-cli.sh ${num}( |$)" >/dev/null 2>&1; then
    return 0
  fi
  if _pgrep_portfolio_agent_for_issue "$num"; then
    return 0
  fi
  return 1
}

strip_agent_labels() {
  local repo="${1:?}"
  local num="${2:?}"
  local label
  for label in agent-done agent-running agent-blocked; do
    gh issue edit "$num" -R "$repo" --remove-label "$label" 2>/dev/null || true
  done
}

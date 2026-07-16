#!/usr/bin/env bash
set -euo pipefail

target="${1:-all}"
if [[ "$target" != "all" && "$target" != "pi" && "$target" != "hermes" ]]; then
  printf 'usage: %s [all|pi|hermes]\n' "$0" >&2
  exit 2
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

classify_failure() {
  local text
  text="$(tr '[:upper:]' '[:lower:]' < "$1")"
  case "$text" in
    *'usage limit'*|*subscription*|*insufficient_quota*|*'quota exceeded'*|*'credit balance'*) printf 'subscription or quota limit' ;;
    *'status 401'*|*'http 401'*|*'status 403'*|*'http 403'*|*unauthorized*|*invalid_grant*|*'authentication failed'*|*'token expired'*) printf 'authentication rejected' ;;
    *'status 429'*|*'http 429'*|*'too many requests'*|*'rate limit'*|*rate_limit*) printf 'rate limited' ;;
    *'connection refused'*|*'connection reset'*|*'network is unreachable'*|*'no such host'*|*'name resolution'*|*'tls handshake timeout'*) printf 'network unavailable' ;;
    *) printf 'unknown provider error' ;;
  esac
}

run_pi_attempt() {
  local provider="$1" model="$2" label="$3"
  local stdout_file="$tmp/pi-${label}.stdout" stderr_file="$tmp/pi-${label}.stderr"
  local session_file="$tmp/pi-${label}.jsonl"
  if "$MULTICA_PI_PATH" -p --mode json --session "$session_file" \
      --provider "$provider" --model "$model" \
      'Reply exactly MULTICA_PI_CANARY_OK and do not use tools.' \
      >"$stdout_file" 2>"$stderr_file" \
      && grep -q 'MULTICA_PI_CANARY_OK' "$stdout_file"; then
    return 0
  fi
  printf '%s\n%s' "$(<"$stdout_file")" "$(<"$stderr_file")" > "$tmp/pi-${label}.combined"
  classify_failure "$tmp/pi-${label}.combined"
  return 1
}

run_pi_canary() {
  local primary="${MULTICA_PI_MODEL:-openai-codex/gpt-5.3-codex}"
  local provider="${primary%%/*}" model="${primary#*/}" category
  if [[ "$provider" == "$model" ]]; then
    provider="openai-codex"
  fi
  if run_pi_attempt "$provider" "$model" primary > "$tmp/pi-primary.category"; then
    printf 'PI PRIMARY PASS\n'
    return 0
  fi
  category="$(<"$tmp/pi-primary.category")"
  printf 'PI PRIMARY FAIL: %s\n' "$category"

  if [[ -z "${FIRTAL_REGISTRY_URL:-}" || -z "${FIRTAL_REGISTRY_KEY:-}" ]]; then
    printf 'PI FALLBACK FAIL: not configured\n'
    return 1
  fi
  if run_pi_attempt firtal-gateway "${FIRTAL_REGISTRY_MODEL:-claude-sonnet-5}" fallback > "$tmp/pi-fallback.category"; then
    printf 'PI FALLBACK PASS\n'
    return 0
  fi
  printf 'PI FALLBACK FAIL: %s\n' "$(<"$tmp/pi-fallback.category")"
  return 1
}

run_hermes_canary() {
  local stdout_file="$tmp/hermes.stdout" stderr_file="$tmp/hermes.stderr"
  if "$MULTICA_HERMES_PATH" chat -q \
      'Reply exactly MULTICA_HERMES_CANARY_OK and do not use tools.' \
      >"$stdout_file" 2>"$stderr_file" \
      && grep -q 'MULTICA_HERMES_CANARY_OK' "$stdout_file"; then
    printf 'HERMES PASS\n'
    return 0
  fi
  printf '%s\n%s' "$(<"$stdout_file")" "$(<"$stderr_file")" > "$tmp/hermes.combined"
  printf 'HERMES FAIL: %s\n' "$(classify_failure "$tmp/hermes.combined")"
  return 1
}

status=0
if [[ "$target" == "all" || "$target" == "pi" ]]; then
  run_pi_canary || status=1
fi
if [[ "$target" == "all" || "$target" == "hermes" ]]; then
  run_hermes_canary || status=1
fi
exit "$status"

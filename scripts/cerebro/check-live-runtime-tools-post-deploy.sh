#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
multica_cli="${MULTICA_POST_DEPLOY_CLI:-$repo_root/server/bin/multica}"
profile="${MULTICA_POST_DEPLOY_PROFILE:-local}"
timeout="${MULTICA_POST_DEPLOY_TIMEOUT:-120}"
interval="${MULTICA_POST_DEPLOY_INTERVAL:-5}"
grace="${MULTICA_POST_DEPLOY_GRACE:-0}"
version_url="${MULTICA_POST_DEPLOY_VERSION_URL:-}"
expected_commit="${MULTICA_POST_DEPLOY_EXPECTED_COMMIT:-}"
cf_access_client_id="${CEREBRO_CF_ACCESS_CLIENT_ID:-}"
cf_access_client_secret="${CEREBRO_CF_ACCESS_CLIENT_SECRET:-}"

is_nonnegative_integer() { [[ "$1" =~ ^(0|[1-9][0-9]*)$ ]]; }
for value in "$timeout" "$interval" "$grace"; do
  if ! is_nonnegative_integer "$value"; then
    printf 'Post-deploy Runtime tool gate: timeout, interval, and grace must be non-negative integers.\n' >&2
    exit 2
  fi
done
if [[ -n "$version_url" || -n "$expected_commit" ]]; then
  if [[ -z "$version_url" || -z "$expected_commit" ]]; then
    printf 'Post-deploy Runtime tool gate: version URL and expected commit must be configured together.\n' >&2
    exit 2
  fi
  if ! command -v curl >/dev/null 2>&1; then
    printf 'Post-deploy Runtime tool gate: curl is required for deployed commit verification.\n' >&2
    exit 2
  fi
fi
if [[ -n "$cf_access_client_id" || -n "$cf_access_client_secret" ]]; then
  if [[ -z "$cf_access_client_id" || -z "$cf_access_client_secret" ]]; then
    printf 'Post-deploy Runtime tool gate: Cloudflare Access client ID and secret must be configured together.\n' >&2
    exit 2
  fi
fi
if [[ ! -x "$multica_cli" ]]; then
  printf 'Post-deploy Runtime tool gate: Multica CLI is not executable at %s.\n' "$multica_cli" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  printf 'Post-deploy Runtime tool gate: jq is required.\n' >&2
  exit 2
fi

last_error=""
commit_observed_at=0

check_deployed_commit() {
  local payload deployed_commit now remaining
  local -a curl_args=(-fsS --max-time 10)
  [[ -n "$version_url" ]] || return 0
  if [[ -n "$cf_access_client_id" ]]; then
    curl_args+=(
      -H "CF-Access-Client-Id: $cf_access_client_id"
      -H "CF-Access-Client-Secret: $cf_access_client_secret"
    )
  fi
  if ! payload="$(curl "${curl_args[@]}" "$version_url" 2>/dev/null)"; then
    last_error="could not read deployed version from $version_url"
    return 1
  fi
  if ! deployed_commit="$(jq -er '.commit | select(type == "string" and length > 0)' <<<"$payload" 2>/dev/null)"; then
    last_error="deployed version response did not contain a commit"
    return 1
  fi
  if [[ "$deployed_commit" != "$expected_commit" ]]; then
    last_error="deployed commit is $deployed_commit; waiting for $expected_commit"
    return 1
  fi
  if [[ "$grace" -gt 0 ]]; then
    now="$(date +%s)"
    if [[ "$commit_observed_at" -eq 0 ]]; then
      commit_observed_at="$now"
    fi
    remaining=$(( grace - (now - commit_observed_at) ))
    if [[ "$remaining" -gt 0 ]]; then
      last_error="deployed commit is current; waiting ${remaining}s for the backend and Runtime reports to settle"
      return 1
    fi
  fi
}

check_live_runtime_tools() {
  local runtimes online_count runtime runtime_id runtime_name capability_count
  if ! runtimes="$("$multica_cli" --profile "$profile" runtime list --output json 2>/dev/null)"; then
    last_error="could not list live workspace Runtimes"
    return 1
  fi
  if ! jq -e 'type == "array"' >/dev/null 2>&1 <<<"$runtimes"; then
    last_error="live workspace Runtime list was not valid JSON"
    return 1
  fi

  online_count="$(jq '[.[] | select((.status // "") == "online")] | length' <<<"$runtimes")"
  if [[ "$online_count" -eq 0 ]]; then
    last_error="no online Runtime was available for live tool verification"
    return 1
  fi

  while IFS= read -r runtime; do
    runtime_id="$(jq -r '.id // empty' <<<"$runtime")"
    runtime_name="$(jq -r '.name // .id // "unnamed Runtime"' <<<"$runtime")"
    if [[ -z "$runtime_id" ]]; then
      last_error="an online Runtime had no identity"
      return 1
    fi
    if ! capability_count="$(jq -er '
      if (.capabilities | type) == "object" then (.capabilities | length) else 0 end
    ' <<<"$runtime" 2>/dev/null)"; then
      last_error="could not read the capability report for $runtime_name ($runtime_id)"
      return 1
    fi
    if [[ "$capability_count" -eq 0 ]]; then
      last_error="$runtime_name ($runtime_id) offers zero tools while online (capability report is empty)"
      return 1
    fi
    printf 'Post-deploy Runtime tool gate: %s (%s) has %s capability fields.\n' "$runtime_name" "$runtime_id" "$capability_count"
  done < <(jq -c '.[] | select((.status // "") == "online")' <<<"$runtimes")
}

deadline=$(( $(date +%s) + timeout ))
while true; do
  if check_deployed_commit && check_live_runtime_tools; then
    printf 'Post-deploy Runtime tool gate: PASS.\n'
    exit 0
  fi
  if [[ "$(date +%s)" -ge "$deadline" ]]; then
    printf 'Post-deploy Runtime tool gate: FAIL — %s.\n' "$last_error" >&2
    exit 1
  fi
  printf 'Post-deploy Runtime tool gate: waiting for live evidence — %s.\n' "$last_error" >&2
  sleep "$interval"
done

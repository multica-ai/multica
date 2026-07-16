#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
canary="$repo_root/scripts/cerebro/runtime-agent-canary.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

printf '%s\n' \
  '#!/bin/sh' \
  'printf '\''%s\n'\'' '\''{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"MULTICA_PI_CANARY_OK"}}'\''' \
  > "$tmp/pi"
printf '%s\n' \
  '#!/bin/sh' \
  'printf '\''%s\n'\'' '\''MULTICA_HERMES_CANARY_OK'\''' \
  > "$tmp/hermes"
chmod +x "$tmp/pi" "$tmp/hermes"

output="$(
  MULTICA_PI_PATH="$tmp/pi" \
  MULTICA_HERMES_PATH="$tmp/hermes" \
  FIRTAL_REGISTRY_URL="https://registry.example.test" \
  FIRTAL_REGISTRY_KEY="synthetic-key" \
  FIRTAL_REGISTRY_MODEL="gateway-test" \
  "$canary" all
)"
grep -q '^PI PRIMARY PASS$' <<<"$output"
grep -q '^HERMES PASS$' <<<"$output"

printf '%s\n' \
  '#!/bin/sh' \
  'case " $* " in' \
  '  *" --provider firtal-gateway "*)' \
  '    printf '\''%s\n'\'' '\''MULTICA_PI_CANARY_OK'\''' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf '\''%s\n'\'' '\''HTTP 401 invalid_grant refresh_token=synthetic-secret-must-not-print'\'' >&2' \
  'exit 1' \
  > "$tmp/pi"
chmod +x "$tmp/pi"

output="$(
  MULTICA_PI_PATH="$tmp/pi" \
  MULTICA_HERMES_PATH="$tmp/hermes" \
  FIRTAL_REGISTRY_URL="https://registry.example.test" \
  FIRTAL_REGISTRY_KEY="synthetic-key" \
  FIRTAL_REGISTRY_MODEL="gateway-test" \
  "$canary" pi
)"
grep -q '^PI PRIMARY FAIL: authentication rejected$' <<<"$output"
grep -q '^PI FALLBACK PASS$' <<<"$output"
if grep -q 'synthetic-secret-must-not-print' <<<"$output"; then
  printf 'canary leaked a synthetic refresh credential\n' >&2
  exit 1
fi

printf 'Runtime canary verifies PI primary/fallback and Hermes without leaking errors\n'

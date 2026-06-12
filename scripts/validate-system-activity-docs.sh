#!/usr/bin/env bash
# Validates that the numeric constants in service.go match what is documented
# in docs/agents/system-activity/wakeup.md. Prevents the docs drifting silently
# from the live code. Run as part of CI (upstream-zone-guard job) or manually.
#
# Exit 0 = consistent. Exit 1 = drift detected.
# Compatible with both GNU and BSD grep (macOS).

set -euo pipefail

SERVICE="server/internal/cerebro/wakeup/service.go"
DOC="docs/agents/system-activity/wakeup.md"

ok=1

check_constant() {
  local name="$1"
  local code_value doc_value

  # Extract value: match "<name> = <digits>" allowing any whitespace around =
  code_value=$(grep -E "${name}[[:space:]]*=[[:space:]]*[0-9]+" "$SERVICE" 2>/dev/null \
    | sed -E "s/.*${name}[[:space:]]*=[[:space:]]*([0-9]+).*/\1/" \
    | head -1 || true)

  if [[ -z "$code_value" ]]; then
    echo "FAIL: ${name} not found in ${SERVICE}"
    ok=0
    return
  fi

  doc_value=$(grep -E "${name}[[:space:]]*=[[:space:]]*${code_value}([^0-9]|$)" "$DOC" 2>/dev/null || true)
  if [[ -z "$doc_value" ]]; then
    echo "FAIL: ${DOC} does not contain '${name} = ${code_value}' (code says ${code_value})"
    ok=0
  else
    echo "OK: ${name} = ${code_value}"
  fi
}

check_constant "WakeupMinIntervalMinutes"
check_constant "WakeupMaxConsecutivePostpones"

if [[ "$ok" -ne 1 ]]; then
  echo ""
  echo "system-activity doc drift detected — update ${DOC} to match ${SERVICE}"
  exit 1
fi

echo "system-activity docs are consistent with code"

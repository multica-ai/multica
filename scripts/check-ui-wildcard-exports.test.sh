#!/usr/bin/env bash
# Proves check-ui-wildcard-exports.mjs actually fails on the case it exists for.
#
# The check guards a blind spot that is invisible by construction: a component
# under a wildcard export registers as a knip entry point, so knip reports it
# as used no matter what. A guard for an invisible failure is worth exactly as
# much as its proof that it still fires, hence this test.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
check="$root_dir/scripts/check-ui-wildcard-exports.mjs"
probe="$root_dir/packages/ui/components/ui/zz-unused-export-probe.tsx"
out="$(mktemp)"

cleanup() {
  rm -f "$probe" "$out"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "Observed:" >&2
  cat "$out" >&2
  exit 1
}

# 1. Clean tree passes. Runs first so a pre-existing dead file is reported as
#    itself rather than surfacing later as a confusing probe failure.
if ! node "$check" >"$out" 2>&1; then
  fail "check should pass on a clean tree"
fi

# 2. A component nothing imports fails the check and is named in the output.
cat >"$probe" <<'EOF'
export function UnusedExportProbe() {
  return null
}
EOF

if node "$check" >"$out" 2>&1; then
  fail "check should fail when packages/ui gains a zero-reference component"
fi

if ! grep -Fq "zz-unused-export-probe.tsx" "$out"; then
  fail "check failed but did not name the offending file"
fi

# 3. Importing it from a consumer clears the check — the guard tracks real
#    usage rather than just counting files.
importer="$root_dir/packages/views/zz-unused-export-probe-importer.tsx"
cat >"$importer" <<'EOF'
export { UnusedExportProbe } from "@multica/ui/components/ui/zz-unused-export-probe";
EOF
trap 'cleanup; rm -f "$importer"' EXIT

if ! node "$check" >"$out" 2>&1; then
  fail "check should pass once a consumer imports the component"
fi

rm -f "$importer" "$probe"
trap cleanup EXIT

echo "PASS: check-ui-wildcard-exports fires on unused components and clears on real imports"

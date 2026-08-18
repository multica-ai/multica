#!/usr/bin/env bash
# Proves check-ui-wildcard-exports.mjs actually fails on the cases it exists for.
#
# The check guards a blind spot that is invisible by construction: a component
# under a wildcard export registers as a knip entry point, so knip reports it
# as used no matter what. A guard for an invisible failure is worth exactly as
# much as its proof that it still fires, hence this test.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
check="$root_dir/scripts/check-ui-wildcard-exports.mjs"
ui_dir="$root_dir/packages/ui/components/ui"
out="$(mktemp)"
scratch=()

cleanup() {
  rm -f "${scratch[@]+"${scratch[@]}"}" "$out"
}
trap cleanup EXIT

track() {
  scratch+=("$1")
}

fail() {
  echo "FAIL: $*" >&2
  echo "Observed:" >&2
  cat "$out" >&2
  exit 1
}

expect_pass() {
  if ! node "$check" >"$out" 2>&1; then fail "$1"; fi
}

expect_fail_naming() {
  if node "$check" >"$out" 2>&1; then fail "$2"; fi
  if ! grep -Fq "$1" "$out"; then fail "check failed but did not name $1"; fi
}

# 1. Clean tree passes. Runs first so a pre-existing dead file is reported as
#    itself rather than surfacing later as a confusing probe failure.
expect_pass "check should pass on a clean tree"

# 2. A component nothing imports fails the check and is named.
probe="$ui_dir/zz-unused-export-probe.tsx"
track "$probe"
cat >"$probe" <<'EOF'
export function UnusedExportProbe() {
  return null
}
EOF
expect_fail_naming "zz-unused-export-probe.tsx" \
  "check should fail when packages/ui gains a zero-reference component"

# 3. Importing it from a consumer clears the check — the guard tracks real
#    usage rather than just counting files.
importer="$root_dir/packages/views/zz-probe-importer.tsx"
track "$importer"
cat >"$importer" <<'EOF'
export { UnusedExportProbe } from "@multica/ui/components/ui/zz-unused-export-probe";
EOF
expect_pass "check should pass once a consumer imports the component"
rm -f "$importer" "$probe"

# 4. Basename collision. 17 of the 59 guarded files share a name with some
#    other file in the repo, so a check that matched relative imports by
#    basename would pass this dead component. `option-card` is real:
#    packages/views/onboarding/components/option-card.tsx is imported as
#    `../components/option-card` and must not vouch for the packages/ui file.
collide="$ui_dir/option-card.tsx"
track "$collide"
cat >"$collide" <<'EOF'
export function OptionCard() {
  return null
}
EOF
expect_fail_naming "components/ui/option-card.tsx" \
  "a same-named file elsewhere must not mark a dead ui component as used"
rm -f "$collide"

# 5. Two dead components importing only each other are still dead. Liveness is
#    reachability from outside the guarded directories, not a reference count.
pair_a="$ui_dir/zz-cycle-a.tsx"
pair_b="$ui_dir/zz-cycle-b.tsx"
track "$pair_a"
track "$pair_b"
cat >"$pair_a" <<'EOF'
import { CycleB } from "./zz-cycle-b";
export function CycleA() {
  return CycleB();
}
EOF
cat >"$pair_b" <<'EOF'
export function CycleB() {
  return null;
}
EOF
expect_fail_naming "zz-cycle-a.tsx" "a mutually-importing dead pair must not vouch for itself"
if ! grep -Fq "zz-cycle-b.tsx" "$out"; then
  fail "both halves of a dead import cycle should be reported"
fi
rm -f "$pair_a" "$pair_b"

expect_pass "check should pass again once every probe is removed"

echo "PASS: check-ui-wildcard-exports resolves real paths, survives name collisions and dead cycles"

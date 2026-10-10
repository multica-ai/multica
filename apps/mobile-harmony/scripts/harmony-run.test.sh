#!/usr/bin/env bash
# Tests for scripts/harmony-run.sh.
#
# The wrapper's contract: it refuses to run when DevEco toolchain pieces are
# missing, runs ohpm install before hvigor, drives hvigor with the right
# module/product/buildMode parameters, installs the produced HAP, and wires
# the reverse port forward for Metro. These tests stub the DevEco binaries
# and assert the call sequence, so they need no DevEco Studio, no device,
# and no node_modules.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-harmony-run.XXXXXX")
export DEVECO_HOME="$TEST_DIR/deveco"
TOOLS="$DEVECO_HOME/Contents/tools"
CALLS_FILE="$TEST_DIR/calls.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

# Lay out the toolchain skeleton the wrapper expects, with recording stubs
# that append straight to the calls log (functions here are not exported
# into the wrapper's subshells).
mkdir -p \
  "$TOOLS/node/bin" \
  "$TOOLS/hvigor/bin" \
  "$TOOLS/ohpm/bin" \
  "$DEVECO_HOME/Contents/sdk/default/openharmony/toolchains" \
  "$DEVECO_HOME/Contents/jbr/Contents/Home"

cat >"$TOOLS/node/bin/node" <<EOF
#!/usr/bin/env bash
set -eu
printf 'node %s %s\n' "\$(basename "\$1")" "\${*:2}" >>"$CALLS_FILE"
EOF
cat >"$TOOLS/ohpm/bin/ohpm" <<EOF
#!/usr/bin/env bash
set -eu
printf 'ohpm %s\n' "\$*" >>"$CALLS_FILE"
EOF
cat >"$DEVECO_HOME/Contents/sdk/default/openharmony/toolchains/hdc" <<EOF
#!/usr/bin/env bash
set -eu
case "\$*" in
  "fport rm"*) exit 0 ;; # stale-forward cleanup is allowed to be a no-op
  *) printf 'hdc %s\n' "\$*" >>"$CALLS_FILE" ;;
esac
EOF
chmod +x "$TOOLS/node/bin/node" \
  "$TOOLS/ohpm/bin/ohpm" \
  "$DEVECO_HOME/Contents/sdk/default/openharmony/toolchains/hdc"
# hvigorw.js only needs to exist; the node stub records its invocation.
: >"$TOOLS/hvigor/bin/hvigorw.js"

# The wrapper's release mode shells out to the workspace react-native CLI;
# stub it where the wrapper expects to find it (project node_modules/.bin).
mkdir -p "$TEST_DIR/node_modules/.bin"
cat >"$TEST_DIR/node_modules/.bin/react-native" <<EOF
#!/usr/bin/env bash
set -eu
printf 'bundle-harmony %s\n' "\$*" >>"$CALLS_FILE"
EOF
chmod +x "$TEST_DIR/node_modules/.bin/react-native"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

# 1. A missing toolchain aborts with a clear error before any tool runs.
DEVECO_HOME="$TEST_DIR/deveco-missing" "$SCRIPT_DIR/harmony-run.sh" \
  >"$TEST_DIR/missing.out" 2>&1 &&
  fail "missing DevEco tools must abort" || true
grep -q "Missing DevEco tool" "$TEST_DIR/missing.out" ||
  fail "missing-tool message not shown"

# 2. Happy path: ohpm install runs before hvigor; hvigor gets the module,
#    product, buildMode and assembleHap parameters; the HAP is installed and
#    the app launched; the reverse forward targets Metro's port.
mkdir -p "$TEST_DIR/harmony/entry/build/default/outputs/default" "$TEST_DIR/scripts"
: >"$TEST_DIR/harmony/entry/build/default/outputs/default/entry-default-unsigned.hap"
: >"$TEST_DIR/harmony/oh-package.json5"
cp "$SCRIPT_DIR/harmony-run.sh" "$TEST_DIR/scripts/harmony-run.sh"

(cd "$TEST_DIR" && METRO_PORT=8099 ./scripts/harmony-run.sh >/dev/null 2>&1)

ohpm_line=$(grep -n "^ohpm install$" "$CALLS_FILE" | cut -d: -f1)
hvigor_line=$(grep -n "^node hvigorw.js" "$CALLS_FILE" | cut -d: -f1)
[ -n "$ohpm_line" ] || fail "ohpm install was not invoked"
[ -n "$hvigor_line" ] || fail "hvigor was not invoked"
[ "$ohpm_line" -lt "$hvigor_line" ] || fail "ohpm install must precede hvigor"
grep -q -- "--mode module" "$CALLS_FILE" || fail "hvigor module mode missing"
grep -q -- "assembleHap" "$CALLS_FILE" || fail "assembleHap target missing"
grep -q "hdc install -r " "$CALLS_FILE" || fail "hdc install missing"
grep -q "rport tcp:8081 tcp:8099" "$CALLS_FILE" || fail "reverse forward missing"
grep -q "aa start -a EntryAbility" "$CALLS_FILE" || fail "app launch missing"

# 3. Embedded mode without MULTICA_API_URL aborts before building anything.
: >"$CALLS_FILE"
(cd "$TEST_DIR" && HARMONY_BUILD_MODE=release ./scripts/harmony-run.sh \
  >"$TEST_DIR/release-missing.out" 2>&1 &&
  fail "embedded mode without MULTICA_API_URL must abort" || true)
grep -q "MULTICA_API_URL" "$TEST_DIR/release-missing.out" ||
  fail "missing MULTICA_API_URL message not shown"
grep -q "^node hvigorw.js" "$CALLS_FILE" &&
  fail "embedded guard must run before hvigor"

# 4. Embedded mode with the env set: a dev plain-JS bundle is generated
#    before hvigor (HBC and minified bundles fail to register in the RNOH
#    0.77 runtime), and hvigor stays on buildMode=debug.
: >"$CALLS_FILE"
(cd "$TEST_DIR" && HARMONY_BUILD_MODE=release MULTICA_API_URL=https://example \
  ./scripts/harmony-run.sh >/dev/null 2>&1)
bundle_line=$(grep -n "^bundle-harmony" "$CALLS_FILE" | cut -d: -f1)
hvigor_line=$(grep -n "^node hvigorw.js" "$CALLS_FILE" | cut -d: -f1)
[ -n "$bundle_line" ] || fail "embedded mode must generate the bundle"
[ "$bundle_line" -lt "$hvigor_line" ] || fail "bundle generation must precede hvigor"
grep -q -- "--dev true" "$CALLS_FILE" || fail "embedded bundle must be a dev bundle"
grep -q -- "--reset-cache" "$CALLS_FILE" ||
  fail "embedded bundle must reset metro cache (env inlining would go stale)"
grep -q -- "--js-engine hermes" "$CALLS_FILE" &&
  fail "hermes bytecode must not be requested (fails to register)"
grep -q -- "buildMode=release" "$CALLS_FILE" &&
  fail "hvigor must stay on debug; the release ArkTS runtime white-screens"

echo "harmony-run.sh tests passed."

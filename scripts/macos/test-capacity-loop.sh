#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FIXTURE="$(mktemp -d -t multica-capacity-loop-test.XXXXXX)"
trap 'rm -rf "$FIXTURE"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

WORKSPACE="11111111-1111-1111-1111-111111111111"
ROOT="$FIXTURE/workspaces"
REMOTE="$FIXTURE/remote.git"
mkdir -p "$ROOT/$WORKSPACE"
git init --bare "$REMOTE" >/dev/null
SEED="$FIXTURE/seed"
git clone "$REMOTE" "$SEED" >/dev/null
git -C "$SEED" config user.email test@example.com
git -C "$SEED" config user.name Test
printf 'seed\n' > "$SEED/tracked.txt"
git -C "$SEED" add tracked.txt
git -C "$SEED" commit -m seed >/dev/null
git -C "$SEED" push -u origin HEAD >/dev/null

make_task() {
  local id="$1" completed="$2"
  local task="$ROOT/$WORKSPACE/$id"
  mkdir -p "$task/workdir"
  printf '{"kind":"issue","issue_id":"%s","workspace_id":"%s","completed_at":"%s"}\n' \
    "$id" "$WORKSPACE" "$completed" > "$task/.gc_meta.json"
}

OLD="2020-01-01T00:00:00Z"
YOUNG="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
make_task aaaaaaaa "$OLD"
git clone "$REMOTE" "$ROOT/$WORKSPACE/aaaaaaaa/workdir/repo" >/dev/null

make_task bbbbbbbb "$OLD"
git clone "$REMOTE" "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo" >/dev/null
printf 'dirty\n' > "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo/unique.txt"
mkdir -p "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo/node_modules/pkg"
printf 'cache\n' > "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo/node_modules/pkg/cache"

make_task cccccccc "$OLD"
git clone "$REMOTE" "$ROOT/$WORKSPACE/cccccccc/workdir/repo" >/dev/null
git -C "$ROOT/$WORKSPACE/cccccccc/workdir/repo" config user.email test@example.com
git -C "$ROOT/$WORKSPACE/cccccccc/workdir/repo" config user.name Test
printf 'ahead\n' >> "$ROOT/$WORKSPACE/cccccccc/workdir/repo/tracked.txt"
git -C "$ROOT/$WORKSPACE/cccccccc/workdir/repo" add tracked.txt
git -C "$ROOT/$WORKSPACE/cccccccc/workdir/repo" commit -m ahead >/dev/null
mkdir -p "$ROOT/$WORKSPACE/cccccccc/workdir/repo/.next/cache"
printf 'cache\n' > "$ROOT/$WORKSPACE/cccccccc/workdir/repo/.next/cache/data"

make_task dddddddd "$OLD"
mkdir -p "$ROOT/$WORKSPACE/dddddddd/workdir/node_modules/pkg"
printf 'active\n' > "$ROOT/$WORKSPACE/dddddddd/workdir/node_modules/pkg/cache"
make_task eeeeeeee "$YOUNG"

COMMON_ENV=(
  "MULTICA_CLEANUP_ROOTS=$ROOT"
  "MULTICA_CLEANUP_FREE_GIB_OVERRIDE=1"
  "MULTICA_CLEANUP_MIN_FREE_GIB=22"
  "MULTICA_CLEANUP_AGE_MINUTES=60"
  "MULTICA_CLEANUP_ACTIVE_PATHS=$ROOT/$WORKSPACE/dddddddd/workdir"
)

env "${COMMON_ENV[@]}" "$SCRIPT_DIR/multica-workspace-cleanup" --dry-run > "$FIXTURE/workspace-dry.jsonl"
grep -q '"path": ".*aaaaaaaa".*"action": "would_delete"\|"action": "would_delete".*"path": ".*aaaaaaaa"' "$FIXTURE/workspace-dry.jsonl" || fail "clean terminal task not selected"
grep -q 'repo_guard:dirty:1' "$FIXTURE/workspace-dry.jsonl" || fail "dirty task not protected"
grep -q 'repo_guard:ahead:1' "$FIXTURE/workspace-dry.jsonl" || fail "ahead task not protected"
grep -q '"reason": "active_path"' "$FIXTURE/workspace-dry.jsonl" || fail "active task not protected"
grep -q '"reason": "ttl_not_elapsed"' "$FIXTURE/workspace-dry.jsonl" || fail "young task not protected"

env "${COMMON_ENV[@]}" "$SCRIPT_DIR/multica-workspace-cleanup" > "$FIXTURE/workspace-real.jsonl"
[ ! -e "$ROOT/$WORKSPACE/aaaaaaaa" ] || fail "clean task not deleted"
[ -e "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo/unique.txt" ] || fail "dirty task changed"
[ -e "$ROOT/$WORKSPACE/cccccccc/workdir/repo/tracked.txt" ] || fail "ahead task changed"
[ -e "$ROOT/$WORKSPACE/dddddddd" ] || fail "active task deleted"
[ -e "$ROOT/$WORKSPACE/eeeeeeee" ] || fail "young task deleted"
[ ! -e "$ROOT/$WORKSPACE/bbbbbbbb/workdir/repo/node_modules" ] || fail "dirty task regenerable artifact not pruned"
[ ! -e "$ROOT/$WORKSPACE/cccccccc/workdir/repo/.next" ] || fail "ahead task regenerable artifact not pruned"
[ -e "$ROOT/$WORKSPACE/dddddddd/workdir/node_modules/pkg/cache" ] || fail "active task artifact pruned"

MARKER="$FIXTURE/insufficient-side-effect"
set +e
MULTICA_HEAVY_BATCH_FREE_GIB_OVERRIDE=21 \
MULTICA_HEAVY_BATCH_STATE_DIR="$FIXTURE/state-insufficient" \
  "$SCRIPT_DIR/multica-heavy-batch" \
  --operation release-amd64 --owner KAP-1184 -- /usr/bin/touch "$MARKER" \
  > "$FIXTURE/heavy-insufficient.log" 2>&1
RC=$?
set -e
[ "$RC" -eq 75 ] || fail "insufficient preflight rc=$RC"
[ ! -e "$MARKER" ] || fail "insufficient preflight created side effect"
grep -q 'side_effects=none' "$FIXTURE/heavy-insufficient.log" || fail "parked result missing"

FAKE_COLIMA="$FIXTURE/fake-colima"
FAKE_DOCKER="$FIXTURE/fake-docker"
cat > "$FAKE_COLIMA" <<'SH'
#!/bin/bash
set -u
case "$1" in
  list) printf 'PROFILE STATUS\n' ;;
  status) [ -e "$FAKE_RUNTIME_STATE/profile-running" ] ;;
  start) touch "$FAKE_RUNTIME_STATE/profile-running" ;;
  ssh)
    if printf '%s\n' "$*" | grep -q fstrim; then
      touch "$FAKE_RUNTIME_STATE/fstrim"
      printf '/: 1073741824 bytes trimmed\n'
    else
      printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
      printf '/dev/test 999999 1 999998 1%% /\n'
    fi
    ;;
  stop) touch "$FAKE_RUNTIME_STATE/stopped"; rm -f "$FAKE_RUNTIME_STATE/profile-running" ;;
  delete) touch "$FAKE_RUNTIME_STATE/deleted"; rm -f "$FAKE_RUNTIME_STATE/profile-running" ;;
  *) exit 0 ;;
esac
SH
cat > "$FAKE_DOCKER" <<'SH'
#!/bin/bash
set -u
case "$*" in
  *" ps -q"*) exit 0 ;;
  *) exit 0 ;;
esac
SH
chmod +x "$FAKE_COLIMA" "$FAKE_DOCKER"

SUCCESS_TEMP="$FIXTURE/tmp.kap-1184.success"
PROTECTED_TEMP="$FIXTURE/tmp.keep"
mkdir -p "$SUCCESS_TEMP" "$PROTECTED_TEMP"
export FAKE_RUNTIME_STATE="$FIXTURE/fake-runtime"
mkdir -p "$FAKE_RUNTIME_STATE"
MULTICA_HEAVY_BATCH_FREE_GIB_OVERRIDE=30 \
MULTICA_HEAVY_BATCH_STATE_DIR="$FIXTURE/state-success" \
MULTICA_HEAVY_BATCH_COLIMA_BIN="$FAKE_COLIMA" \
MULTICA_HEAVY_BATCH_DOCKER_BIN="$FAKE_DOCKER" \
  "$SCRIPT_DIR/multica-heavy-batch" \
  --operation release-amd64 --owner KAP-1184 \
  --profile kap1184-amd64 --temp-root "$SUCCESS_TEMP" \
  -- /usr/bin/true \
  > "$FIXTURE/heavy-success.log"
[ ! -e "$SUCCESS_TEMP" ] || fail "success temp root not removed"
[ -e "$PROTECTED_TEMP" ] || fail "unowned sibling removed"
[ -e "$FAKE_RUNTIME_STATE/fstrim" ] || fail "fstrim not called"
[ -e "$FAKE_RUNTIME_STATE/deleted" ] || fail "batch-owned idle profile not deleted"

FAIL_TEMP="$FIXTURE/tmp.kap-1184.failure"
mkdir -p "$FAIL_TEMP"
set +e
MULTICA_HEAVY_BATCH_FREE_GIB_OVERRIDE=30 \
MULTICA_HEAVY_BATCH_STATE_DIR="$FIXTURE/state-failure" \
  "$SCRIPT_DIR/multica-heavy-batch" \
  --operation next-build --owner KAP-1184 --temp-root "$FAIL_TEMP" \
  -- /bin/sh -c 'exit 7' > "$FIXTURE/heavy-failure.log"
RC=$?
set -e
[ "$RC" -eq 7 ] || fail "controlled failure rc=$RC"
[ ! -e "$FAIL_TEMP" ] || fail "failure temp root not removed"
[ -e "$PROTECTED_TEMP" ] || fail "failure removed unowned sibling"

FAKE_PREVIEW_DOCKER="$FIXTURE/fake-preview-docker"
cat > "$FAKE_PREVIEW_DOCKER" <<'SH'
#!/bin/bash
set -u
if printf '%s\n' "$*" | grep -q 'ps -aq'; then
  printf 'expired\nactive\n'
elif printf '%s\n' "$*" | grep -q 'inspect expired'; then
  printf '[{"Name":"/expired","Config":{"Labels":{"ai.multica.owner":"KAP-1184","ai.multica.expires-at":"2020-01-01T00:00:00Z"}},"State":{"Status":"exited"}}]\n'
elif printf '%s\n' "$*" | grep -q 'inspect active'; then
  printf '[{"Name":"/active","Config":{"Labels":{"ai.multica.owner":"KAP-1184","ai.multica.expires-at":"2020-01-01T00:00:00Z"}},"State":{"Status":"running"}}]\n'
elif printf '%s\n' "$*" | grep -q 'rm expired'; then
  touch "$FAKE_PREVIEW_STATE/expired-removed"
elif printf '%s\n' "$*" | grep -q 'ps -aq'; then
  exit 0
elif printf '%s\n' "$*" | grep -q 'images --no-trunc'; then
  exit 0
fi
SH
chmod +x "$FAKE_PREVIEW_DOCKER"
FAKE_PREVIEW_COLIMA="$FIXTURE/fake-preview-colima"
cat > "$FAKE_PREVIEW_COLIMA" <<'SH'
#!/bin/bash
set -u
case "$1" in
  ssh) touch "$FAKE_PREVIEW_STATE/fstrim" ;;
  delete) touch "$FAKE_PREVIEW_STATE/profile-deleted" ;;
esac
SH
chmod +x "$FAKE_PREVIEW_COLIMA"
export FAKE_PREVIEW_STATE="$FIXTURE/fake-preview-state"
mkdir -p "$FAKE_PREVIEW_STATE"
MULTICA_PREVIEW_PROFILES=kap1184-amd64 \
MULTICA_PREVIEW_DOCKER_BIN="$FAKE_PREVIEW_DOCKER" \
MULTICA_PREVIEW_COLIMA_BIN="$FAKE_PREVIEW_COLIMA" \
  "$SCRIPT_DIR/multica-preview-cleanup" > "$FIXTURE/preview.log"
[ -e "$FAKE_PREVIEW_STATE/expired-removed" ] || fail "expired terminal preview not removed"
grep -q '"reason": "active:running"' "$FIXTURE/preview.log" || fail "active preview not protected"
[ ! -e "$FAKE_PREVIEW_STATE/profile-deleted" ] || fail "profile with active preview deleted"

printf 'PASS: capacity loop protection and cleanup tests\n'

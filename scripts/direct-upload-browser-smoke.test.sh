#!/usr/bin/env bash
# Safety regression for scripts/direct-upload-browser-smoke.sh.
#
# That script creates and destroys a database, a container and three server
# processes. A bug in HOW it names or removes them can delete a parallel run's
# resources — or, with a caller-supplied name, a real one. These checks exercise
# that logic WITHOUT creating anything: they source the script's naming,
# preflight and cleanup code against a stubbed `docker`, then assert on the
# stub's call log.
#
# Covered:
#   1. Names are unique per run (two runs never share a database, container,
#      bucket or workspace slug).
#   2. An existing database is REFUSED, not dropped and recreated.
#   3. An existing container is REFUSED, not removed and reused.
#   4. Cleanup removes only what this run recorded as owned.
#   5. A caller-supplied, non-empty SMOKE_DIR is refused.
#   6. A failed preflight creates and destroys nothing.
#   7. The workspace slug stays a usable URL/identifier segment.
#
# Usage: bash scripts/direct-upload-browser-smoke.test.sh
set -uo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SMOKE_SCRIPT="$SCRIPT_DIR/direct-upload-browser-smoke.sh"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PASS=0
FAIL=0
FAILURES=()

ok()   { PASS=$((PASS + 1)); printf '    \033[32m✓\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '    \033[31m✗\033[0m %s\n' "$1"; }
step() { printf '\n==> %s\n' "$1"; }

TMP=$(mktemp -d "${TMPDIR:-/tmp}/direct-upload-browser-smoke-test.XXXXXX") || exit 1
trap 'rm -rf "$TMP"' EXIT

[ -f "$SMOKE_SCRIPT" ] || { echo "missing $SMOKE_SCRIPT" >&2; exit 1; }

# ------------------------------------------------------------- name uniqueness

step "Resource names are unique per run"
# Pull the naming block out of the real script so this test cannot drift from
# what ships: the run id is the only thing that differs between two runs.
naming_source="$(sed -n '/^RAW_RUN_ID=/,/^SMOKE_SLUG=/p' "$SMOKE_SCRIPT" \
  | sed -e 's/^RAW_RUN_ID=.*/RAW_RUN_ID="$1"/')"

names_for_run_id() {
  bash -c '
    set -uo pipefail
    eval "$2"
    printf "%s\n%s\n%s\n%s\n" "$DB_NAME" "$S3_CONTAINER" "$S3_BUCKET" "$SMOKE_SLUG"
  ' _ "$1" "$naming_source"
}

first="$(names_for_run_id '1700000000-111-0a1b2c3d')"
second="$(names_for_run_id '1700000001-222-deadbeef')"
if [ "$first" = "$second" ]; then
  bad "two runs produced identical resource names: $first"
else
  ok "two runs produce distinct database, container, bucket and slug names"
fi

db_from_first="$(printf '%s\n' "$first" | sed -n 1p)"
case "$db_from_first" in
  multica_browser_*) ok "database name keeps its owned prefix: $db_from_first" ;;
  *) bad "database name lost its prefix: $db_from_first" ;;
esac
if [ "${#db_from_first}" -le 60 ]; then
  ok "database name fits the identifier limit (${#db_from_first} chars)"
else
  bad "database name is ${#db_from_first} chars, over the 60-char limit"
fi

slug_from_first="$(printf '%s\n' "$first" | sed -n 4p)"
case "$slug_from_first" in
  browser-*) ok "workspace slug keeps its owned prefix: $slug_from_first" ;;
  *) bad "workspace slug lost its prefix: $slug_from_first" ;;
esac
# A slug lands in a URL path and a reserved-name check; punctuation there would
# make the browser fixture's navigation fail for a reason unrelated to uploads.
if printf '%s' "$slug_from_first" | grep -qE '^[a-z0-9-]+$'; then
  ok "workspace slug is a valid URL segment"
else
  bad "workspace slug contains characters a URL segment cannot carry: $slug_from_first"
fi

hostile="$(names_for_run_id 'a;b$(rm -rf /)c')"
if printf '%s' "$hostile" | grep -qE '[:;()$ ]'; then
  bad "a hostile run id leaked shell syntax into a resource name: $hostile"
else
  ok "a hostile run id is sanitized before it reaches a resource name"
fi

# ------------------------------------------------ existing resources refused

step "An existing database is refused, never dropped"
STUB_DIR="$TMP/stub"
mkdir -p "$STUB_DIR"
cat > "$STUB_DIR/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  inspect)
    case "$*" in
      *State.Running*) printf 'true\n' ;;
    esac
    exit 0
    ;;
  exec)
    case "$*" in
      *"SELECT 1 FROM pg_database"*) printf '1\n'; exit 0 ;;
      *printenv*) printf 'multica\n'; exit 0 ;;
    esac
    exit 0
    ;;
  *) exit 0 ;;
esac
STUB
chmod +x "$STUB_DIR/docker"

DOCKER_LOG="$TMP/existing-db.log"
: > "$DOCKER_LOG"
set +e
(
  export PATH="$STUB_DIR:$PATH"
  export DOCKER_LOG
  export SMOKE_DIR="$TMP/existing-db-run"
  mkdir -p "$SMOKE_DIR"
  export SMOKE_PG_CONTAINER=stub-postgres
  # The extracted copy sits outside the checkout; point it at the real one so
  # its dependency probe measures the workspace, not /tmp.
  export SMOKE_REPO_ROOT="$REPO_ROOT"
  # Everything up to (not including) the object-storage section.
  sed -n '1,/^# --------------------------------------------------------- object storage/p' "$SMOKE_SCRIPT" \
    | sed '$d' > "$TMP/preflight-only.sh"
  bash "$TMP/preflight-only.sh" 2>&1
) >"$TMP/existing-db.out" 2>&1
status=$?
set -e
if [ $status -ne 0 ]; then
  ok "preflight refuses an existing database (exit $status)"
else
  bad "preflight accepted an existing database"
fi
if grep -qi 'DROP DATABASE' "$DOCKER_LOG"; then
  bad "preflight issued a DROP for a database it did not create:
$(grep -i 'DROP DATABASE' "$DOCKER_LOG")"
else
  ok "preflight never issued a DROP against the existing database"
fi
if grep -qi 'refusing to touch it' "$TMP/existing-db.out"; then
  ok "the refusal explains itself"
else
  bad "the refusal message is missing: $(tail -3 "$TMP/existing-db.out")"
fi

step "An existing container is refused, never removed"
cat > "$STUB_DIR/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  inspect)
    case "$*" in
      *State.Running*) printf 'true\n' ;;
    esac
    exit 0
    ;;
  exec)
    case "$*" in
      *"SELECT 1 FROM pg_database"*) exit 0 ;;
      *printenv*) printf 'multica\n'; exit 0 ;;
    esac
    exit 0
    ;;
  *) exit 0 ;;
esac
STUB
chmod +x "$STUB_DIR/docker"

DOCKER_LOG="$TMP/existing-container.log"
: > "$DOCKER_LOG"
set +e
(
  export PATH="$STUB_DIR:$PATH"
  export DOCKER_LOG
  export SMOKE_DIR="$TMP/existing-container-run"
  mkdir -p "$SMOKE_DIR"
  export SMOKE_PG_CONTAINER=stub-postgres
  sed -n '1,/^# Bucket + CORS with the backend/p' "$SMOKE_SCRIPT" > "$TMP/with-s3.sh"
  bash "$TMP/with-s3.sh" 2>&1
) >"$TMP/existing-container.out" 2>&1
status=$?
set -e
if [ $status -ne 0 ]; then
  ok "object-storage startup refuses an existing container (exit $status)"
else
  bad "object-storage startup accepted an existing container"
fi
if grep -qE '^rm .*-f ' "$DOCKER_LOG"; then
  bad "startup removed a container by name:
$(grep -E '^rm ' "$DOCKER_LOG")"
else
  ok "startup never removed a pre-existing container"
fi

step "Cleanup removes only what this run created"
DOCKER_LOG="$TMP/cleanup.log"
: > "$DOCKER_LOG"
set +e
(
  export PATH="$STUB_DIR:$PATH"
  export DOCKER_LOG
  owned_dir="$TMP/cleanup-run"
  mkdir -p "$owned_dir/logs"
  SMOKE_DIR="$owned_dir"
  # Extract the ownership helpers AND cleanup() verbatim from the script under
  # test. The helpers initialize (truncate) the ledger, so it is written AFTER.
  # shellcheck disable=SC1090
  eval "$(sed -n '/^OWNED_FILE=/,/^owned_of_kind()/p' "$SMOKE_SCRIPT")"
  eval "$(sed -n '/^cleanup() {/,/^}/p' "$SMOKE_SCRIPT")"
  printf 'container\tmine-s3\n' > "$owned_dir/owned.tsv"
  printf 'database\tmine_db\n' >> "$owned_dir/owned.tsv"
  FAIL=0
  PASS=0
  declare -a FAILURES=()
  step() { :; }
  info() { :; }
  ok() { :; }
  bad() { :; }
  SMOKE_KEEP=0
  SMOKE_KEEP_DIR=1     # do not rm a directory this harness owns
  PG_CONTAINER=stub-postgres
  POSTGRES_USER=multica
  API_PID=""
  PROXY_PID=""
  WEB_PID=""
  RUN_ID=test
  DB_NAME=mine_db
  cleanup
) >"$TMP/cleanup.out" 2>&1
set -e
removed_containers="$(grep -E '^rm ' "$DOCKER_LOG" || true)"
if printf '%s' "$removed_containers" | grep -q 'mine-s3'; then
  ok "cleanup removed the container this run created"
else
  bad "cleanup did not remove its own container: $removed_containers"
fi
dropped="$(grep -iE 'DROP DATABASE' "$DOCKER_LOG" || true)"
if printf '%s' "$dropped" | grep -q 'mine_db'; then
  ok "cleanup dropped the database this run created"
else
  bad "cleanup did not drop its own database: $dropped"
fi
# A destructive command may only name the one container and the one database in
# the ledger. Exact tokens, so the `psql -d postgres` maintenance connection does
# not read as an unrelated target.
removed_names="$(printf '%s\n' "$removed_containers" | sed -n 's/^rm -f //p' | tr ' ' '\n' | grep -v '^$' | sort -u)"
dropped_names="$(printf '%s\n' "$dropped" | sed -n 's/.*DROP DATABASE IF EXISTS "\{0,1\}\([^"[:space:]]*\)"\{0,1\}.*/\1/p' | sort -u)"
unexpected="$(printf '%s\n%s\n' "$removed_names" "$dropped_names" | grep -v '^$' | grep -vxE 'mine-s3|mine_db' || true)"
if [ -n "$unexpected" ]; then
  bad "cleanup named resources outside its ledger: $unexpected"
else
  ok "cleanup named nothing outside its ownership ledger"
fi

step "A caller-supplied non-empty SMOKE_DIR is refused"
set +e
(
  export SMOKE_DIR="$TMP/not-empty"
  mkdir -p "$SMOKE_DIR"
  : > "$SMOKE_DIR/somebody-elses-evidence"
  bash "$SMOKE_SCRIPT" 2>&1
) >"$TMP/busy-dir.out" 2>&1
status=$?
set -e
if [ $status -ne 0 ]; then
  ok "a non-empty SMOKE_DIR is refused (exit $status)"
else
  bad "a non-empty SMOKE_DIR was accepted"
fi
if [ -f "$TMP/not-empty/somebody-elses-evidence" ]; then
  ok "the caller's file survived"
else
  bad "the caller's file was deleted"
fi

step "A failed preflight creates nothing"
DOCKER_LOG="$TMP/failed-preflight.log"
: > "$DOCKER_LOG"
cat > "$STUB_DIR/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOCKER_LOG"
[ "$1" = inspect ] && exit 1
case "$1" in
  exec)
    case "$*" in
      *printenv*) printf 'multica\n'; exit 0 ;;
    esac
    exit 0
    ;;
esac
exit 0
STUB
chmod +x "$STUB_DIR/docker"
set +e
(
  export PATH="$STUB_DIR:$PATH"
  export DOCKER_LOG
  export SMOKE_DIR="$TMP/failed-preflight-run"
  mkdir -p "$SMOKE_DIR"
  export SMOKE_PG_CONTAINER=missing-postgres
  bash "$SMOKE_SCRIPT" 2>&1
) >"$TMP/failed-preflight.out" 2>&1
status=$?
set -e
if [ $status -ne 0 ]; then
  ok "preflight fails when its database server is absent (exit $status)"
else
  bad "preflight succeeded without a database server"
fi
if grep -qE '^rm |^run |DROP DATABASE' "$DOCKER_LOG"; then
  bad "a failed preflight still touched resources:
$(cat "$DOCKER_LOG")"
else
  ok "a failed preflight created and destroyed nothing"
fi

# ------------------------------------------------------------------- summary

printf '\n'
if [ "$FAIL" -gt 0 ]; then
  printf '\033[31mFAILED: %d check(s)\033[0m\n' "$FAIL"
  for failure in "${FAILURES[@]}"; do printf '  - %s\n' "$failure"; done
  exit 1
fi
printf '\033[32mALL %d SAFETY CHECKS PASSED\033[0m\n' "$PASS"

#!/usr/bin/env bash
# Self-host wait must fail when the backend container has already exited, and
# must keep reporting "still starting" when the container is up but /health has
# not answered yet. No real docker: a stub on PATH answers the two compose
# queries the script makes.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# Args: <state|missing|broken> <label>
# state: printed by `ps --all` (exited, running, dead, ...).
# missing: `ps -q` finds no container.
# broken: every compose query exits non-zero.
run_case() {
  local state=$1 label=$2
  local tmp stub out err status
  tmp="$(mktemp -d)"
  stub="$tmp/bin"
  mkdir -p "$stub"

  # COMPOSE="docker compose" arrives as: docker compose <files...> <verb> ...
  # The verb is matched on a whole argument. A substring check would treat the
  # compose file path (it contains no verb, but a loose grep did match "ps"
  # inside "selfhost") as the verb and answer the wrong query.
  cat >"$stub/docker" <<STUB
#!/usr/bin/env bash
set -euo pipefail
verb=""
for arg in "\$@"; do
  case "\$arg" in
    port|ps) verb="\$arg" ;;
  esac
done
case "\$verb" in
  port)
    # A published port, so the script does not fall back to a default.
    printf '0.0.0.0:18080\n'
    exit 0
    ;;
  ps)
    ;;
  *)
    echo "stub docker: unexpected invocation: \$*" >&2
    exit 2
    ;;
esac

case "${state}" in
  broken)
    exit 1
    ;;
esac

# ps -q backend proves a container exists. ps --all --format reports State.
for arg in "\$@"; do
  if [ "\$arg" = "-q" ]; then
    case "${state}" in
      missing) exit 0 ;;
      *) printf 'abc123backend\n'; exit 0 ;;
    esac
  fi
done
for arg in "\$@"; do
  if [ "\$arg" = "--all" ]; then
    printf '%s\n' "${state}"
    exit 0
  fi
done
echo "stub docker: ps query not recognized: \$*" >&2
exit 2
STUB
  chmod +x "$stub/docker"

  cat >"$stub/curl" <<'STUB'
#!/usr/bin/env bash
# Health never answers, so the script always takes the timeout path.
exit 22
STUB
  chmod +x "$stub/curl"

  out="$tmp/out"
  err="$tmp/err"
  set +e
  PATH="$stub:$PATH" \
    COMPOSE="docker compose" \
    SELFHOST_WAIT_ATTEMPTS=1 \
    SELFHOST_WAIT_INTERVAL_SECONDS=0 \
    bash "$ROOT_DIR/scripts/selfhost-wait.sh" >"$out" 2>"$err"
  status=$?
  set -e

  printf '%s\n' "$status" >"$tmp/status"
  printf '%s\n' "$out" >"$tmp/outpath"
  printf '%s\n' "$err" >"$tmp/errpath"
  printf '%s\n' "$label" >"$tmp/label"
  # Caller reads these; trap in the caller removes the dir.
  printf '%s\n' "$tmp"
}

assert_dead() {
  local state=$1
  local tmp status out
  tmp="$(run_case "$state" "dead:$state")"
  status="$(cat "$tmp/status")"
  out="$(cat "$(cat "$tmp/outpath")")"
  if [ "$status" -eq 0 ]; then
    echo "$out"
    fail "state '$state' exited 0; a dead backend must be non-zero"
  fi
  printf '%s\n' "$out" | grep -q "backend container has exited" \
    || fail "state '$state' did not say the backend container has exited"
  printf '%s\n' "$out" | grep -q "docker compose -f docker-compose.selfhost.yml logs" \
    || fail "state '$state' did not print the logs command"
  printf '%s\n' "$out" | grep -q "Services are still starting" \
    && fail "state '$state' still claimed services are starting"
  rm -rf "$tmp"
  echo "ok  exited non-zero with the logs command when backend is $state"
}

assert_alive() {
  local state=$1
  local tmp status out
  tmp="$(run_case "$state" "alive:$state")"
  status="$(cat "$tmp/status")"
  out="$(cat "$(cat "$tmp/outpath")")"
  if [ "$status" -ne 0 ]; then
    echo "$out"
    cat "$(cat "$tmp/errpath")" >&2 || true
    fail "state '$state' exited $status; a slow start must stay 0"
  fi
  printf '%s\n' "$out" | grep -q "Services are still starting. Check logs:" \
    || fail "state '$state' dropped the still-starting message"
  printf '%s\n' "$out" | grep -q "docker compose -f docker-compose.selfhost.yml logs" \
    || fail "state '$state' did not print the logs command"
  printf '%s\n' "$out" | grep -q "backend container has exited" \
    && fail "state '$state' reported a dead container"
  rm -rf "$tmp"
  echo "ok  exited 0 and kept the still-starting hint when backend is $state"
}

assert_dead exited
assert_dead dead
assert_dead created
assert_dead removing
assert_alive running
assert_alive restarting
assert_alive missing
assert_alive broken

echo "selfhost-wait: all cases passed"

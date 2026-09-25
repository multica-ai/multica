#!/usr/bin/env bash
# Proves perf-compare.mjs reports honestly and leaves nothing running.
#
# The comparison is only worth running if its verdict can be trusted and it
# cleans up after itself, and both have failed before: a report left by an
# earlier run was read back as today's result, a scenario that reported `ok`
# and then failed still exited 0, a server child that stopped answering was
# taken for a closed port and outlived the script, and teardown once waited
# forever on the server it was meant to stop.
#
# This drives the real script with a fake `pnpm` on PATH — no build, no
# browser, a few seconds — through each of those cases.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compare="$root_dir/scripts/perf-compare.mjs"
work="$(mktemp -d)"
repo="$work/repo"
bin="$work/bin"
server_log="$work/servers.jsonl"
mkdir -p "$repo" "$bin" "$work/tmp"
touch "$server_log"

# Only ever signal processes this test's own fake servers recorded.
kill_recorded() {
  node -e '
    const fs = require("fs");
    for (const line of fs.readFileSync(process.argv[1], "utf8").split("\n")) {
      if (!line) continue;
      try { process.kill(JSON.parse(line).pid, "SIGKILL"); } catch {}
    }' "$server_log" || true
}

cleanup() {
  kill_recorded
  rm -rf "$work"
}
trap cleanup EXIT

git -C "$repo" init -q
echo fixture >"$repo/fixture.txt"
git -C "$repo" add fixture.txt
git -C "$repo" -c user.name=test -c user.email=test@example.test commit -qm fixture

# Stands in for pnpm: install and build succeed, `start` runs a tiny server,
# and `playwright` behaves as PERF_PLAYWRIGHT says.
cat >"$bin/pnpm" <<'NODEEOF'
#!/usr/bin/env node
const { createServer } = require("node:http");
const { spawn } = require("node:child_process");
const { appendFileSync, writeFileSync } = require("node:fs");
const args = process.argv.slice(2);
const record = (pid) =>
  appendFileSync(process.env.PERF_TEST_SERVER_LOG, `${JSON.stringify({ pid })}\n`);

if (args[0] === "install") process.exit(0);
if (args[0] === "exec" && args[1] === "turbo") { console.log("fake build"); process.exit(0); }

if (args[0] === "--filter" && args[2] === "start") {
  const port = Number(process.env.PORT);
  if (process.env.PERF_SERVER === "stubborn") {
    // A child in the wrapper's process group that ignores SIGTERM and stops
    // answering while keeping the port — the wrapper itself exits promptly.
    const child = spawn(process.execPath, ["-e", `
      const { createServer } = require("node:http");
      let stopping = false;
      process.on("SIGTERM", () => { stopping = true; });
      createServer((req, res) => { if (!stopping) res.end("ok"); })
        .listen(${port}, "127.0.0.1");
    `], { stdio: "ignore" });
    record(child.pid);
    process.on("SIGTERM", () => process.exit(0));
    setInterval(() => {}, 1 << 30);
  } else {
    record(process.pid);
    createServer((req, res) => res.end("ok")).listen(port, "127.0.0.1");
  }
  return;
}

if (args[0] === "exec" && args[1] === "playwright") {
  const report = {
    status: "ok", invalid: [], typing_elapsed_ms: 123,
    fixture: { version: "test", sha256: "fresh", bytes: 0 },
  };
  const mode = process.env.PERF_PLAYWRIGHT;
  if (mode !== "fail-no-report") writeFileSync(process.env.PERF_REPORT_PATH, JSON.stringify(report));
  process.exit(mode === "ok" ? 0 : 1);
}
process.exit(2);
NODEEOF
chmod +x "$bin/pnpm"

fail() { echo "FAIL [$case]: $*" >&2; cat "$work/out.log" >&2 || true; exit 1; }

# run_case <name> <playwright mode> <server mode> [base ref]
run_case() {
  case="$1"
  local out="$work/out-$1"
  mkdir -p "$out"
  if [ -n "${SEED_STALE:-}" ]; then
    for side in base head; do
      echo '{"status":"ok","invalid":[],"typing_elapsed_ms":999,"fixture":{"sha256":"stale"}}' >"$out/$side.json"
    done
    echo "stale summary" >"$out/comparison.md"
  fi
  : >"$server_log"
  set +e
  (cd "$repo" && PATH="$bin:$PATH" TMPDIR="$work/tmp" PERF_STOP_GRACE_MS=300 \
    PERF_PLAYWRIGHT="$2" PERF_SERVER="$3" PERF_TEST_SERVER_LOG="$server_log" \
    node "$compare" --base "${4:-HEAD}" --head HEAD --out "$out") >"$work/out.log" 2>&1
  code=$?
  set -e
  comparison="$out/comparison.json"
  [ -f "$comparison" ] || fail "no comparison.json written"
  live="$(node -e '
    const fs = require("fs");
    let n = 0;
    for (const line of fs.readFileSync(process.argv[1], "utf8").split("\n")) {
      if (!line) continue;
      try { process.kill(JSON.parse(line).pid, 0); n++; } catch {}
    }
    console.log(n);' "$server_log")"
  [ "$live" = 0 ] || fail "$live server process(es) still running"
  [ -z "$(ls -A "$work/tmp")" ] || fail "temporary checkouts left behind"
  [ "$(git -C "$repo" worktree list | wc -l | tr -d ' ')" = 1 ] || fail "worktrees left behind"
}

field() { node -e 'const r = require(process.argv[1]); console.log(eval("r." + process.argv[2]) ?? "")' "$comparison" "$1"; }
expect_exit() { [ "$code" = "$1" ] || fail "expected exit $1, got $code"; }
expect_status() { for side in base head; do [ "$(field "$side.status")" = "$1" ] || fail "$side status: expected $1, got $(field "$side.status")"; done; }

run_case success ok normal
expect_exit 0
expect_status ok

run_case failure-without-report fail-no-report normal
expect_exit 1
expect_status invalid

SEED_STALE=1 run_case stale-report fail-no-report normal
expect_exit 1
expect_status invalid
[ "$(field base.typing_elapsed_ms)" != 999 ] || fail "the previous run's report was read as this run's"
[ ! -f "$work/out-stale-report/comparison.md" ] || grep -vq "stale summary" "$work/out-stale-report/comparison.md" \
  || fail "the previous run's summary survived"

run_case failure-after-ok-report fail-after-report normal
expect_exit 1
expect_status invalid
grep -q "process exited" "$comparison" || fail "the invalid verdict does not say the process failed"

run_case server-ignores-sigterm ok stubborn
expect_exit 0
expect_status ok

SEED_STALE=1 run_case unknown-ref ok normal does-not-exist
expect_exit 1
[ ! -f "$work/out-unknown-ref/comparison.md" ] || fail "an aborted run left the previous summary in place"
grep -q "comparison aborted\|error" "$comparison" || fail "the aborted run does not record why"

echo "perf-compare: all lifecycle and reporting cases passed"

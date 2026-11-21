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
call_log="$work/calls.jsonl"
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

api_pid=""
cleanup() {
  kill_recorded
  [ -z "$api_pid" ] || kill "$api_pid" 2>/dev/null || true
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

// What each step saw, for the core-scenario cases to check.
const note = (step, extra = {}) => process.env.PERF_TEST_CALL_LOG &&
  appendFileSync(process.env.PERF_TEST_CALL_LOG, `${JSON.stringify({ step, args, ...extra })}\n`);

if (args[0] === "install") process.exit(0);
if (args[0] === "exec" && args[1] === "turbo") {
  note("build", { api: process.env.NEXT_PUBLIC_API_URL ?? null, ws: process.env.NEXT_PUBLIC_WS_URL ?? null });
  console.log("fake build");
  process.exit(0);
}

if (args[0] === "--filter" && args[2] === "start") {
  note("start", { remote: process.env.REMOTE_API_URL, api: process.env.NEXT_PUBLIC_API_URL ?? null, ws: process.env.NEXT_PUBLIC_WS_URL ?? null });
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
  note("playwright", { scenario: process.env.PERF_SCENARIO, ref: process.env.PERF_REF, label: process.env.PERF_LABEL });
  const core = (process.env.PERF_SCENARIO ?? "").startsWith("core-");
  // The core spec's report carries no status of its own.
  const report = core ? {
    ref: process.env.PERF_REF, label: process.env.PERF_LABEL,
    dataset: { version: "core-test", digest: process.env.PERF_TEST_DIGEST?.split(",")[process.env.PERF_LABEL === "base" ? 0 : 1] ?? "same", counts: { issues: 1 } },
    environment: { browser: "test", viewport: "1440x900", cpu_throttle: 1, cpu: "test", cores: 1, repeats: 1, inbox_rows: "20", inbox_until: "comment" },
    summary: { issue: { large: { open_content_ms: 841 } }, inbox: {} },
    samples: { issue: [], inbox: [] },
  } : {
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
  : >"$call_log"
  (cd "$repo" && PATH="$bin:$PATH" TMPDIR="$work/tmp" PERF_STOP_GRACE_MS=300 \
    PERF_PLAYWRIGHT="$2" PERF_SERVER="$3" PERF_TEST_SERVER_LOG="$server_log" PERF_TEST_CALL_LOG="$call_log" \
    node "$compare" --base "${4:-HEAD}" --head HEAD --out "$out" ${SCENARIO:+--scenario "$SCENARIO"}) >"$work/out.log" 2>&1
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

# Core scenarios (MUL-9539) run against a real local API instead of synthetic
# HTTP. Outside `make env-exec` there is none, and the script says so up front.
SCENARIO=core-paths
case=core-without-api-env
set +e
(cd "$repo" && PATH="$bin:$PATH" env -u MULTICA_DEV_PROFILE -u DATABASE_URL -u PORT \
  node "$compare" --base HEAD --head HEAD --out "$work/out-core-no-env" --scenario core-paths) >"$work/out.log" 2>&1
code=$?
set -e
expect_exit 2
grep -q "make env-exec" "$work/out.log" || fail "does not say how to provide the API environment"
[ "$(git -C "$repo" worktree list | wc -l | tr -d ' ')" = 1 ] || fail "built something without an API"

# A stand-in for the local API: only /health is ever asked for.
api_port="$(node -e 'const s = require("net").createServer().listen(0, "127.0.0.1", () => { console.log(s.address().port); s.close(); })')"
node -e 'require("http").createServer((q, r) => { r.statusCode = q.url === "/health" ? 200 : 404; r.end("ok"); }).listen(Number(process.argv[1]), "127.0.0.1")' "$api_port" &
api_pid=$!
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$api_port/health" >/dev/null 2>&1 && break; sleep 0.1; done
# `make env-exec` also exports the API's direct URLs; the core builds must not use them.
export MULTICA_DEV_PROFILE=dev-test DATABASE_URL=postgres://localhost/perf_test PORT="$api_port"
export NEXT_PUBLIC_API_URL="http://localhost:$api_port" NEXT_PUBLIC_WS_URL="ws://localhost:$api_port/ws"

run_case core-success ok normal
expect_exit 0
expect_status ok
calls() { node -e '
  const fs = require("fs");
  const rows = fs.readFileSync(process.argv[1], "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
  console.log(eval(process.argv[2]));' "$call_log" "$1"; }
[ "$(calls 'rows.filter((r) => r.step === "build" || r.step === "start").every((r) => r.api === "" && r.ws === "")')" = true ] \
  || fail "builds or servers did not call the API on their own origin"
[ "$(calls 'rows.filter((r) => r.step === "start").every((r) => r.remote === "http://localhost:'"$api_port"'")')" = true ] \
  || fail "servers were not pointed at the local API"
[ "$(calls 'rows.filter((r) => r.step === "playwright").every((r) => !r.args.some((a) => a.startsWith("--repeat-each")) && r.args.includes("core-paths.spec.ts") && r.scenario === "core-paths")')" = true ] \
  || fail "the core spec was not run once, with its own rounds"
[ "$(calls 'rows.filter((r) => r.step === "playwright").map((r) => r.label + ":" + (r.ref ?? "").length).join(",")')" = "base:10,head:10" ] \
  || fail "the reports were not labelled with their side and ref"
md="$work/out-core-success/comparison.md"
grep -q "Open a big issue: \`large\`" "$md" || fail "comparison.md lacks the core-path table"
grep -q "| Open → first comments visible (ms) | 841 | 841 |" "$md" || fail "comparison.md lacks the measured numbers"

run_case core-spec-failed fail-after-report normal
expect_exit 1
expect_status invalid
grep -q "process exited" "$comparison" || fail "the invalid verdict does not say the process failed"

PERF_TEST_DIGEST=one,two run_case core-different-datasets ok normal
expect_exit 1
grep -q "different datasets" "$work/out-core-different-datasets/comparison.md" || fail "different datasets were compared"

kill "$api_pid" 2>/dev/null || true
wait "$api_pid" 2>/dev/null || true
api_pid=""
case=core-api-down
set +e
(cd "$repo" && PATH="$bin:$PATH" node "$compare" --base HEAD --head HEAD --out "$work/out-core-api-down" --scenario core-paths) >"$work/out.log" 2>&1
code=$?
set -e
expect_exit 2
grep -q "not healthy" "$work/out.log" || fail "an unreachable API was not reported"
unset SCENARIO MULTICA_DEV_PROFILE DATABASE_URL PORT NEXT_PUBLIC_API_URL NEXT_PUBLIC_WS_URL

echo "perf-compare: all lifecycle and reporting cases passed"

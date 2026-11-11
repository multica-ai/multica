import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { buildReport, classifyTool, readRollout, renderTable } from "./issue-activity-usage.mjs";

const start = "2026-01-01T00:00:10Z", end = "2026-01-01T00:00:20Z";
const issue = { id: "issue-1", identifier: "TEST-1" };
const tokens = (input = 70, output = 10, read = 20, write = 10) => ({ input_tokens: input, output_tokens: output, cache_read_tokens: read, cache_write_tokens: write });
const raw = (input = 100, output = 10, read = 20, write = 10) => ({ input_tokens: input, output_tokens: output, cached_input_tokens: read, cache_write_input_tokens: write });
const run = (usage = [{ provider: "codex", model: "model-1", ...tokens() }]) => ({ id: "run-1", started_at: start, completed_at: end, status: "completed", attempt: 1, result: { session_id: "session-1" }, usage });
const summary = (value = tokens(), reported = 1) => ({ task_count: reported, unreported_task_count: reported ? 0 : 1, ...Object.fromEntries(Object.entries(value).map(([key, n]) => [`total_${key}`, n])) });
const event = (type, payload, seconds = 11) => ({ type, timestamp: `2026-01-01T00:00:${seconds}Z`, payload });
const snapshot = (value = raw(), seconds = 12) => event("event_msg", { type: "token_count", info: { total_token_usage: value } }, seconds);
const tool = (name = "exec_command", args = { cmd: "rg usage server" }, seconds = 11) => event("response_item", { type: "function_call", name, arguments: JSON.stringify(args) }, seconds);

function fixture(t, events, metaTime = start) {
  const dir = mkdtempSync(join(tmpdir(), "activity-usage-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const path = join(dir, "rollout.jsonl");
  writeFileSync(path, [event("session_meta", { id: "session-1", timestamp: metaTime }, 10), event("turn_context", { model: "model-1" }, 10), ...events].map((row) => JSON.stringify(row)).join("\n"));
  return path;
}

test("classifies explicit tool actions and keeps mixed or dynamic calls unclassified", () => {
  for (const [cmd, expected] of [
    ["rg usage server", "planning"], ["go test ./pkg/agent", "development"],
    ['"$MULTICA_CLI_PATH" issue comment add TEST-1 --content-file reply.md', "agent_communication"],
    ["gh pr checks 12", "ci_check"], ["glab api projects/7/jobs/4/trace", "ci_check"],
    ["glab api --hostname git.example.com projects/7/pipelines/4", "ci_check"],
    ["gh api repos/example/repo/actions/runs/4/jobs", "ci_check"],
    ["cat pipeline-notes.md", "planning"], ["echo gh pr checks 12", "other_unclassified"],
    ["rg test . && go test ./pkg/agent", "other_unclassified"],
    ["cat <<'EOF'\ngh pr checks 12\nEOF", "other_unclassified"],
  ]) assert.equal(classifyTool("exec_command", { cmd }), expected, cmd);
  assert.equal(classifyTool("exec", 'await tools.exec_command({cmd:"rg usage server"});'), "planning");
  assert.equal(classifyTool("exec", 'await tools.exec_command({cmd:"rg usage server"}); await tools.apply_patch(patch);'), "other_unclassified");
  assert.equal(classifyTool("exec", 'await tools.exec_command({cmd:command});'), "other_unclassified");
  assert.equal(classifyTool("exec", 'text("tools.apply_patch(patch)");'), "other_unclassified");
  assert.equal(classifyTool("exec", 'text("tools.apply_patch(patch)"); await tools.exec_command({cwd:"repo",cmd:"rg usage server"});'), "planning");
  assert.equal(classifyTool("exec", null), "other_unclassified");
});

test("uses exclusive cache buckets, counts cumulative snapshots once, and retains line evidence", async (t) => {
  const path = fixture(t, [tool(), snapshot(), snapshot(), tool("apply_patch", {}, 13), snapshot(raw(200, 25, 50, 20), 14)]);
  const result = await readRollout(path, run());
  assert.equal(result.calls.length, 2);
  assert.deepEqual(result.models["model-1"], tokens(130, 25, 50, 20));
  assert.deepEqual(result.calls.map((row) => row.activity), ["planning", "development"]);
  assert.deepEqual(result.calls[0].tool_lines, [3]);
});

test("isolates a resumed run and excludes later runs in the same session", async (t) => {
  const path = fixture(t, [snapshot(raw(500, 100, 200, 20), "09"), tool(), snapshot(raw(600, 110, 220, 30)), snapshot(raw(900, 150, 300, 50), 21)], "2026-01-01T00:00:01Z");
  const result = await readRollout(path, run());
  assert.deepEqual(result.models["model-1"], tokens());
  assert.equal(result.calls.length, 1);
});

test("completion timestamps serialized to seconds include the final fractional-second response", async (t) => {
  const path = fixture(t, [tool(), snapshot(raw(), "20.500")]);
  assert.deepEqual((await readRollout(path, run())).models["model-1"], tokens());
});

test("refuses a resumed lifetime total without its baseline and warns on counter resets", async (t) => {
  const path = fixture(t, [tool(), snapshot(), snapshot(raw(10, 20, 2, 1), 13)], "2026-01-01T00:00:01Z");
  const result = await readRollout(path, run());
  assert.match(result.warnings.join(" "), /baseline/);
  assert.match(result.warnings.join(" "), /Counter reset/);
  assert.deepEqual(result.calls[0].tokens, tokens(7, 10, 2, 1));
});

test("reconciles all four activities inside one run without distributing by time or text length", async (t) => {
  const path = fixture(t, [
    tool(), snapshot(), tool("apply_patch", {}, 13), snapshot(raw(200, 20, 40, 20), 14),
    tool("exec_command", { cmd: '"$MULTICA_CLI_PATH" issue comment list TEST-1' }, 15), snapshot(raw(300, 30, 60, 30), 16),
    tool("exec_command", { cmd: "gh pr checks 12" }, 17), snapshot(raw(400, 40, 80, 40), 18),
  ]);
  const value = tokens(280, 40, 80, 40);
  const report = await buildReport(issue, summary(value), [run([{ provider: "codex", model: "model-1", ...value }])], new Map([["run-1", path]]));
  assert.deepEqual(report.reconciliation, { source_vs_runs: "matched", activities_vs_runs: "matched" });
  assert.deepEqual(report.activities.map((row) => row.share_percent), [25, 25, 25, 25, 0]);
  assert.deepEqual(report.activities[0].tokens, tokens());
  assert.equal(report.runs[0].evidence.length, 4);
});

test("mixed actions and assistant-only responses keep their measured tokens unclassified", async (t) => {
  const path = fixture(t, [tool(), tool("apply_patch"), snapshot(), snapshot(raw(200, 20, 40, 20), 13)]);
  const result = await readRollout(path, run());
  assert.deepEqual(result.calls.map((row) => row.activity), ["other_unclassified", "other_unclassified"]);
});

test("missing local logs and other providers preserve reported totals as unclassified", async () => {
  const report = await buildReport(issue, summary(), [run([{ provider: "claude", model: "model-1", ...tokens() }])]);
  assert.deepEqual(report.activities[4].tokens, tokens());
  assert.equal(report.activities[4].share_percent, 100);
  assert.equal(report.runs[0].models[0].reconciliation, "no_call_usage");
});

test("stored zeros without call-level evidence are unverified, not proof of no token usage", async () => {
  const value = tokens(100, 0, 20, 0);
  const report = await buildReport(issue, summary(value), [run([{ provider: "claude", model: "model-1", ...value }])]);
  assert.deepEqual(report.unverified_zero_counters, ["output_tokens", "cache_write_tokens"]);
  assert.equal(report.source_totals.output_tokens, 0);
  assert.equal(report.coverage.lower_bound, true);
  assert.match(renderTable(report), /미확인\(저장0\)/);
});

test("a rollout mismatch stays unclassified and a source mismatch has no percentage", async (t) => {
  const path = fixture(t, [tool(), snapshot(raw(150))]);
  const report = await buildReport(issue, summary(tokens(80)), [run()], new Map([["run-1", path]]));
  assert.equal(report.runs[0].models[0].reconciliation, "mismatch");
  assert.equal(report.reconciliation.source_vs_runs, "mismatch");
  assert.equal(report.activities[0].share_percent, null);
  assert.deepEqual(report.activities[4].tokens, tokens());
});

test("unreported failed and active runs are missing rather than measured zero", async () => {
  const failed = { ...run([]), status: "failed" };
  const active = { ...run([]), id: "run-2", status: "running", completed_at: null };
  const report = await buildReport(issue, summary(tokens(0, 0, 0, 0), 0), [failed, active]);
  assert.equal(report.source_totals.input_tokens, null);
  assert.equal(report.activities[0].tokens.input_tokens, null);
  assert.equal(report.runs[0].tokens.output_tokens, null);
  assert.equal(report.coverage.active_runs, 1);
  assert.equal(report.coverage.lower_bound, true);
  assert.match(renderTable(report), /미보고/);
});

test("missing cache details, malformed logs, and wrong sessions cannot manufacture measured zeros", async (t) => {
  const incomplete = raw(); delete incomplete.cache_write_input_tokens;
  const path = fixture(t, [tool(), snapshot(incomplete)]);
  const report = await buildReport(issue, summary(), [run()], new Map([["run-1", path]]));
  assert.deepEqual(report.activities[4].tokens, tokens());
  assert.match(report.runs[0].rollout.warnings[0], /Incomplete/);
  await assert.rejects(() => readRollout(path, { ...run(), result: { session_id: "another-session" } }), /does not match/);
  writeFileSync(path, "{broken\n");
  await assert.rejects(() => readRollout(path, run()), /Invalid rollout JSON/);
});

test("duplicate runs, duplicate usage rows, and cross-issue rollouts fail instead of doubling usage", async () => {
  await assert.rejects(() => buildReport(issue, summary(), [run(), run()]), /Duplicate run/);
  await assert.rejects(() => buildReport(issue, summary(), [run([...run().usage, ...run().usage])]), /Duplicate usage row/);
  await assert.rejects(() => buildReport(issue, summary(), [], new Map([["other-run", "ignored"]])), /outside this issue/);
});

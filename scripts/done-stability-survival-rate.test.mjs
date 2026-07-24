import assert from "node:assert/strict";
import test from "node:test";

import {
  buildLaunchdPlist,
  classifyIssue,
  defaultReportDir,
  defaultReportPath,
  evaluateCommandResult,
  extractRecheckCommands,
  inferSemanticAssertion,
  isSafeRecheckCommand,
  isRetryableMulticaError,
  normalizeReportRow,
  normalizeIssueRecord,
  parseArgs,
  summarizeReport,
} from "./done-stability-survival-rate.mjs";

test("classifies fix and migration issues while excluding Allen closeout reports", () => {
  const fix = classifyIssue({
    title: "WS-1309 OG runtime修复",
    description: "Root cause ENOENT; acceptance: runtime fresh",
  });
  assert.equal(fix.isTarget, true);
  assert.match(fix.matchedTerms.join(" "), /修复|runtime|ENOENT/);

  const closeout = classifyIssue({
    title: "[艾伦] HTML 化 + 飞书通知: WS-1309 OG runtime修复",
    description: "Please turn parent issue into an HTML close-out report.",
  });
  assert.equal(closeout.isTarget, false);
  assert.match(closeout.reason, /excluded/i);
});

test("excludes audit, SOP, daily report, and loop design work from target fixes", () => {
  for (const issue of [
    { title: "D 线架构审计 - 2026-06-23", description: "检查失败率和任务流闭环率" },
    { title: "旺店通退货入库 SOP 沉淀", description: "按物流单号搜索-审核-委外推送" },
    { title: "[每日简报 2026-06-21]", description: "生成报告" },
    { title: "[Loop 设计 v2] 小龙 跟自己 Codex 设计 1 个业务 Loop", description: "业务 loop 设计" },
  ]) {
    assert.equal(classifyIssue(issue).isTarget, false, issue.title);
  }
});

test("excludes onboarding, sync, review, and smoke-test work from target fixes", () => {
  for (const issue of [
    { title: "A2A同步：Codex 浏览器协作边界 v1", description: "复述修复边界" },
    { title: "[CLI 普及] 维欣 本机装飞书 CLI + 钉钉 CLI", description: "报错就贴证据" },
    { title: "CTO 复核 Record & Replay", description: "error 计数复查" },
    { title: "[WS-538·Phase2] 旺店通正式建单单笔冒烟", description: "报错后记录" },
    { title: "夜间自纠错 2026-06-22", description: "失败项整理" },
    { title: "Hermes hotfix Stage 1 ping smoke test", description: "capacity error auto-retry" },
  ]) {
    assert.equal(classifyIssue(issue).isTarget, false, issue.title);
  }
});

test("extracts shell recheck commands from fenced and prompted markdown", () => {
  const commands = extractRecheckCommands(`验收命令:

\`\`\`bash
multica issue get WS-1309 --output json
launchctl print gui/501/ai.multica.daemon | rg runtime
\`\`\`

Then run:
$ ps aux | rg '[m]ultica'
`);

  assert.deepEqual(commands, [
    "multica issue get WS-1309 --output json",
    "launchctl print gui/501/ai.multica.daemon | rg runtime",
    "ps aux | rg '[m]ultica'",
  ]);
});

test("allows read-only acceptance commands and rejects mutating commands", () => {
  assert.equal(isSafeRecheckCommand("multica issue get WS-1309 --output json").safe, true);
  assert.equal(isSafeRecheckCommand("launchctl print gui/501/ai.multica.daemon").safe, true);
  assert.equal(isSafeRecheckCommand("ps aux | rg '[m]ultica'").safe, true);

  assert.equal(isSafeRecheckCommand("multica issue status WS-1309 done").safe, false);
  assert.equal(isSafeRecheckCommand("rm -rf /tmp/example").safe, false);
});

test("rejects placeholder or write-producing commands", () => {
  assert.equal(isSafeRecheckCommand("ls -l /var/folders/.../sky/event_stream").safe, false);
  assert.equal(isSafeRecheckCommand("launchctl print … state=running").safe, false);
  assert.equal(isSafeRecheckCommand("multica workspace get 95fe92d1...").safe, false);
  assert.equal(isSafeRecheckCommand("multica issue get/comment list").safe, false);
  assert.equal(isSafeRecheckCommand("multica attachment download 019edc03-42fe -o .").safe, false);
});

test("rejects incomplete command fragments", () => {
  assert.equal(isSafeRecheckCommand("multica issue get").safe, false);
  assert.equal(isSafeRecheckCommand("ps etime").safe, false);
});

test("normalizes issue rows with updated_at as the documented done_at fallback", () => {
  const issue = normalizeIssueRecord({
    id: "abc",
    identifier: "WS-1",
    title: "fix runtime",
    description: "done",
    status: "done",
    updated_at: "2026-06-20T00:00:00Z",
  });

  assert.deepEqual(issue, {
    id: "abc",
    identifier: "WS-1",
    title: "fix runtime",
    description: "done",
    status: "done",
    updated_at: "2026-06-20T00:00:00Z",
    done_at: "2026-06-20T00:00:00Z",
    done_at_source: "updated_at",
  });
});

test("summarizes survival rate as pass divided by total evaluated issues", () => {
  const summary = summarizeReport([
    { status: "pass" },
    { status: "fail" },
    { status: "pass" },
    { status: "skipped" },
  ]);

  assert.deepEqual(summary, {
    total: 4,
    evaluated: 3,
    skipped: 1,
    pass: 2,
    fail: 1,
    survival_rate: 0.6667,
  });
});

test("normalizes legacy boolean rows into tri-state rows", () => {
  assert.deepEqual(normalizeReportRow({ pass: true }), { status: "pass", pass: true });
  assert.deepEqual(normalizeReportRow({ pass: false }), { status: "fail", pass: false });
  assert.deepEqual(normalizeReportRow({ status: "skipped" }), { status: "skipped", pass: null });
});

test("requires semantic assertions instead of accepting bare exit zero", () => {
  assert.deepEqual(inferSemanticAssertion("date", ""), {
    kind: "unsupported",
    reason: "no semantic assertion can be inferred",
  });

  const bareExitZero = evaluateCommandResult({
    command: "date",
    assertion: inferSemanticAssertion("date", ""),
    result: { exitCode: 0, stdoutTail: "Thu Jul 2", stderrTail: "", timedOut: false },
  });
  assert.equal(bareExitZero.status, "skipped");
  assert.equal(bareExitZero.pass, null);
});

test("infers concrete grep assertions from command output", () => {
  const assertion = inferSemanticAssertion("launchctl print gui/501/com.multica.daemon | rg 'state = running'", "");
  assert.deepEqual(assertion, {
    kind: "stdout_matches",
    pattern: "state = running",
    reason: "pipeline grep/rg pattern must appear in stdout",
  });

  const pass = evaluateCommandResult({
    command: "launchctl print gui/501/com.multica.daemon | rg 'state = running'",
    assertion,
    result: { exitCode: 0, stdoutTail: "state = running", stderrTail: "", timedOut: false },
  });
  assert.equal(pass.status, "pass");

  const fail = evaluateCommandResult({
    command: "launchctl print gui/501/com.multica.daemon | rg 'state = running'",
    assertion,
    result: { exitCode: 0, stdoutTail: "state = waiting", stderrTail: "", timedOut: false },
  });
  assert.equal(fail.status, "fail");
});

test("infers standalone grep and rg assertions", () => {
  assert.deepEqual(inferSemanticAssertion("rg 'rows_touched' /tmp/report.json", ""), {
    kind: "stdout_matches",
    pattern: "rows_touched",
    reason: "standalone grep/rg pattern must appear in stdout",
  });
});

test("prefers semantically assertable commands over unsupported high-score reads", async () => {
  const { pickRecheckCommand } = await import("./done-stability-survival-rate.mjs");
  const commands = [
    "multica issue get WS-1309 --output json",
    "rg 'rows_touched' /tmp/report.json",
  ];

  assert.equal(pickRecheckCommand(commands, ""), "rg 'rows_touched' /tmp/report.json");
});

test("accepts the default unlimited recheck cap", () => {
  const options = parseArgs([]);

  assert.equal(options.maxRechecks, Infinity);
});

test("defaults report output to durable multica reports directory", () => {
  assert.match(defaultReportDir(), /\/\.multica\/reports\/done-stability-survival-rate$/);
  assert.match(
    defaultReportPath(new Date("2026-07-02T00:00:00Z")),
    /\/\.multica\/reports\/done-stability-survival-rate\/done-stability-survival-rate-2026-07-02\.json$/,
  );

  const options = parseArgs(["--output-dir", "/tmp/done-stability"]);
  assert.equal(options.output, "/tmp/done-stability/done-stability-survival-rate-2026-07-02.json");
});

test("builds launchd plist for daily retained reports", () => {
  const plist = buildLaunchdPlist({
    label: "ai.multica.done-stability-survival-rate",
    nodePath: "/usr/local/bin/node",
    scriptPath: "/repo/scripts/done-stability-survival-rate.mjs",
    outputDir: "/Users/example/.multica/reports/done-stability-survival-rate",
    logDir: "/Users/example/.multica/logs",
    hour: 9,
    minute: 15,
    environment: {
      PATH: "/opt/homebrew/bin:/usr/bin:/bin",
      OPENAI_API_KEY: "",
    },
  });

  assert.match(plist, /<key>Label<\/key>\s*<string>ai\.multica\.done-stability-survival-rate<\/string>/);
  assert.match(plist, /<string>--output-dir<\/string>/);
  assert.match(plist, /<string>\/Users\/example\/\.multica\/reports\/done-stability-survival-rate<\/string>/);
  assert.match(plist, /<key>Hour<\/key>\s*<integer>9<\/integer>/);
  assert.match(plist, /<key>Minute<\/key>\s*<integer>15<\/integer>/);
  assert.match(plist, /<key>EnvironmentVariables<\/key>/);
  assert.match(plist, /<key>PATH<\/key>\s*<string>\/opt\/homebrew\/bin:\/usr\/bin:\/bin<\/string>/);
  assert.match(plist, /<key>OPENAI_API_KEY<\/key>\s*<string><\/string>/);
});

test("treats CLI deadline timeouts as retryable", () => {
  assert.equal(
    isRetryableMulticaError("context deadline exceeded (Client.Timeout or context cancellation while reading body)"),
    true,
  );
  assert.equal(isRetryableMulticaError("issue not found"), false);
});

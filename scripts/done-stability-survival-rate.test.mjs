import assert from "node:assert/strict";
import test from "node:test";

import {
  classifyIssue,
  extractRecheckCommands,
  isSafeRecheckCommand,
  isRetryableMulticaError,
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
    { pass: true },
    { pass: false },
    { pass: true },
  ]);

  assert.deepEqual(summary, {
    total: 3,
    pass: 2,
    fail: 1,
    survival_rate: 0.6667,
  });
});

test("accepts the default unlimited recheck cap", () => {
  const options = parseArgs([]);

  assert.equal(options.maxRechecks, Infinity);
});

test("treats CLI deadline timeouts as retryable", () => {
  assert.equal(
    isRetryableMulticaError("context deadline exceeded (Client.Timeout or context cancellation while reading body)"),
    true,
  );
  assert.equal(isRetryableMulticaError("issue not found"), false);
});

#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createReadStream } from "node:fs";
import { glob } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { promisify, parseArgs } from "node:util";

export const activities = ["development", "planning", "agent_communication", "ci_check", "other_unclassified"];
const fields = ["input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"];
const zero = () => Object.fromEntries(fields.map((field) => [field, 0]));
const missing = () => Object.fromEntries(fields.map((field) => [field, null]));
const add = (a, b) => Object.fromEntries(fields.map((field) => [field, a[field] + b[field]]));
const equal = (a, b) => fields.every((field) => a[field] === b[field]);
const sum = (tokens) => fields.reduce((total, field) => total + tokens[field], 0);
const count = (value) => Number.isSafeInteger(value) && value >= 0;
const terminal = new Set(["completed", "failed", "cancelled"]);

function execActions(code) {
  const tokens = code.match(/\s+|\/\/[^\n]*|\/\*[\s\S]*?\*\/|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[A-Za-z_$][\w$]*|[^\s]/g)
    ?.filter((token) => !/^\s|^\/\//.test(token) && !token.startsWith("/*")) ?? [];
  if (tokens.includes("`")) return ["other_unclassified"];
  const actions = [];
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i] !== "tools") continue;
    if (tokens[i + 1] !== "." || tokens[i + 3] !== "(") return ["other_unclassified"];
    if (tokens[i + 2] === "apply_patch") { actions.push("development"); continue; }
    if (tokens[i + 2] !== "exec_command" || tokens[i + 4] !== "{") return ["other_unclassified"];
    let command;
    for (let j = i + 5; j < tokens.length && tokens[j] !== "}"; j++) {
      if (!["cmd", '"cmd"', "'cmd'"].includes(tokens[j]) || tokens[j + 1] !== ":") continue;
      const literal = tokens[j + 2] ?? "";
      if (!/^(['"])[\s\S]*\1$/.test(literal) || /\\(?![\\'"nrt])/.test(literal)) break;
      command = literal.slice(1, -1).replace(/\\([\\'"nrt])/g, (_, char) => ({ n: "\n", r: "\r", t: "\t" })[char] ?? char);
      break;
    }
    actions.push(classifyTool("exec_command", { cmd: command }));
  }
  return actions;
}

// Classify only tool actions, never prompts, tool results, or quoted prose.
export function classifyTool(name, input) {
  if (/^(?:functions\.)?apply_patch$/.test(name)) return "development";
  if (/^(?:functions\.)?exec$/.test(name)) {
    if (typeof input !== "string") return "other_unclassified";
    const actions = execActions(input);
    return actions.length && new Set(actions).size === 1 ? actions[0] : "other_unclassified";
  }
  if (!/^(?:functions\.)?exec_command$/.test(name)) return "other_unclassified";
  const command = input?.cmd ?? input?.command;
  if (typeof command !== "string") return "other_unclassified";
  // Shell scripts, substitutions, pipelines, and here-documents need intent we cannot infer.
  if (/[\n;|&<>`]/.test(command) || command.includes("$(")) return "other_unclassified";
  const text = command.trim();
  if (/^(?:"?\$MULTICA_CLI_PATH"?|multica)\s+issue\s+(?:comment|create|update|assign|status|children|runs)\b/.test(text)) return "agent_communication";
  if (/^(?:gh\s+(?:pr\s+checks|run\s+(?:view|list|watch))|glab\s+(?:ci|pipeline)\b|glab\s+api\s+(?:--hostname\s+\S+\s+)?\S*\/(?:pipelines|jobs)(?:\/|\?|\s|$)|gh\s+api\s+(?:--hostname\s+\S+\s+)?repos\/\S+\/actions\/(?:runs|jobs)\b)/.test(text)) return "ci_check";
  if (/^(?:git\s+(?:add|commit|push|cherry-pick|merge|rebase|apply)\b|(?:go|pnpm|npm|yarn|cargo|make)\s+(?:test\S*|build|check\S*|lint|typecheck)\b)/.test(text)) return "development";
  if (/^(?:(?:rg|cat|sed|head|tail|ls|pwd)\b|git\s+(?:show|diff|status|log|ls-files)\b|(?:"?\$MULTICA_CLI_PATH"?|multica)\s+issue\s+(?:get|usage|search)\b)/.test(text)) return "planning";
  return "other_unclassified";
}

function rawCounters(raw) {
  if (!raw) return null;
  const values = [raw.input_tokens, raw.output_tokens, raw.cached_input_tokens ?? raw.cache_read_input_tokens, raw.cache_write_input_tokens];
  return values.every(count) ? values : null;
}

function normalized(raw) {
  if (raw[0] < raw[2] + raw[3]) return null;
  return { input_tokens: raw[0] - raw[2] - raw[3], output_tokens: raw[1], cache_read_tokens: raw[2], cache_write_tokens: raw[3] };
}

export async function readRollout(path, run) {
  const result = { calls: [], warnings: [], models: {} };
  let lineNumber = 0, previous, model, meta, toolActions = [], toolLines = [];
  const start = Date.parse(run.started_at);
  // The API can serialize completion at second precision; include that whole second.
  const end = run.completed_at ? Date.parse(run.completed_at) + (/T\d\d:\d\d:\d\dZ$/.test(run.completed_at) ? 999 : 0) : Infinity;
  if (!Number.isFinite(start) || Number.isNaN(end)) throw new Error("Run has no valid execution window");
  const lines = createInterface({ input: createReadStream(path), crlfDelay: Infinity });
  for await (const line of lines) {
    lineNumber++;
    let event;
    try { event = JSON.parse(line); }
    catch { throw new Error(`Invalid rollout JSON at line ${lineNumber}`); }
    const payload = event.payload ?? {};
    if (event.type === "session_meta") {
      meta = payload;
      if (!run.result?.session_id || (payload.id ?? payload.session_id) !== run.result.session_id) throw new Error("Rollout session does not match the run");
      continue;
    }
    if (!meta) throw new Error("Rollout must start with session_meta");
    const time = Date.parse(event.timestamp);
    const inWindow = time > start && time <= end;
    if (event.type === "turn_context") model = payload.model;
    if (event.type === "response_item" && ["function_call", "custom_tool_call"].includes(payload.type) && inWindow) {
      let input = payload.input ?? payload.arguments;
      if (payload.type === "function_call" && typeof input === "string") {
        try { input = JSON.parse(input); } catch { input = null; }
      }
      toolActions.push(classifyTool(payload.name ?? "", input));
      toolLines.push(lineNumber);
    }
    if (event.type !== "event_msg" || payload.type !== "token_count" || !payload.info) continue;
    const current = rawCounters(payload.info.total_token_usage);
    if (!current || !Number.isFinite(time)) {
      result.warnings.push(`Incomplete token snapshot at line ${lineNumber}`);
      previous = undefined;
      toolActions = []; toolLines = [];
      continue;
    }
    if (time <= start) { previous = current; toolActions = []; toolLines = []; continue; }
    if (time > end) break;
    // A resumed log needs a pre-run baseline; its lifetime total is not run usage.
    const baselineMissing = !previous && Date.parse(meta.timestamp) < start;
    const delta = previous ? current.map((value, i) => value >= previous[i] ? value - previous[i] : value) : current;
    if (previous?.some((value, i) => current[i] < value)) result.warnings.push(`Counter reset at line ${lineNumber}`);
    previous = current;
    const tokens = normalized(delta);
    if (baselineMissing || !tokens) {
      result.warnings.push(`Unusable baseline or cache counters at line ${lineNumber}`);
      toolActions = []; toolLines = [];
      continue;
    }
    if (sum(tokens) === 0) continue;
    const kinds = new Set(toolActions);
    const activity = kinds.size === 1 ? toolActions[0] : "other_unclassified";
    const call = { line: lineNumber, tool_lines: toolLines, model: payload.info.model ?? model ?? null, activity, tokens };
    result.calls.push(call);
    if (call.model) result.models[call.model] = add(result.models[call.model] ?? zero(), tokens);
    toolActions = []; toolLines = [];
  }
  if (!meta) throw new Error("Empty rollout");
  return result;
}

export async function buildReport(issue, usage, runs, rolloutPaths = new Map()) {
  const buckets = Object.fromEntries(activities.map((activity) => [activity, zero()]));
  const reports = [], seen = new Set();
  let runTotal = zero(), measuredRuns = 0;
  for (const run of runs) {
    if (seen.has(run.id)) throw new Error(`Duplicate run ${run.id}`);
    seen.add(run.id);
    const rows = run.usage ?? [];
    const report = { run_id: run.id, status: run.status, attempt: run.attempt, tokens: rows.length ? zero() : missing(), coverage: rows.length ? "reported" : "unreported", models: [], evidence: [] };
    let rollout;
    if (rolloutPaths.has(run.id)) rollout = await readRollout(rolloutPaths.get(run.id), run);
    report.rollout = rollout ? { path: rolloutPaths.get(run.id), warnings: rollout.warnings } : null;
    const seenModels = new Set();
    for (const row of rows) {
      if (!fields.every((field) => count(row[field]))) throw new Error(`Incomplete usage row for run ${run.id}`);
      const key = `${row.provider}/${row.model}`;
      if (seenModels.has(key)) throw new Error(`Duplicate usage row for run ${run.id}`);
      seenModels.add(key);
      const tokens = Object.fromEntries(fields.map((field) => [field, row[field]]));
      report.tokens = add(report.tokens, tokens);
      const observed = row.provider === "codex" ? rollout?.models[row.model] : null;
      const matched = observed && equal(observed, tokens) && rollout.warnings.length === 0;
      report.models.push({ provider: row.provider, model: row.model, tokens, observed_tokens: observed ?? null, reconciliation: matched ? "matched" : observed ? "mismatch" : "no_call_usage" });
      if (matched) {
        for (const call of rollout.calls.filter((call) => call.model === row.model)) {
          buckets[call.activity] = add(buckets[call.activity], call.tokens);
          report.evidence.push(call);
        }
      } else buckets.other_unclassified = add(buckets.other_unclassified, tokens);
    }
    if (rows.length) { runTotal = add(runTotal, report.tokens); measuredRuns++; }
    if (!rows.length && rollout) report.live_observed_tokens = rollout.models;
    reports.push(report);
  }
  for (const runID of rolloutPaths.keys()) if (!seen.has(runID)) throw new Error(`Rollout names a run outside this issue: ${runID}`);
  const reported = usage.task_count > 0;
  const source = reported ? Object.fromEntries(fields.map((field) => [field, usage[`total_${field}`]])) : missing();
  if (reported && !fields.every((field) => count(source[field]))) throw new Error("Issue usage is missing supported counters");
  const matched = reported && equal(source, runTotal);
  const denominator = matched ? sum(source) : null;
  const allocated = Object.values(buckets).reduce(add, zero());
  return {
    issue: { id: issue.id, identifier: issue.identifier }, generated_at: new Date().toISOString(),
    scope: "this_issue_only; all attempts/statuses; excludes child issues and direct-room chat runs",
    token_basis: "provider_reported; disjoint uncached input, output (includes reasoning), cache read, cache write; not billing cost",
    attribution: "tool_rules_v1_estimate; mixed/dynamic/no-tool responses are unclassified; no proportional allocation",
    source_totals: source, run_totals: measuredRuns ? runTotal : missing(),
    reconciliation: { source_vs_runs: reported ? matched ? "matched" : "mismatch" : "unreported", activities_vs_runs: measuredRuns ? equal(allocated, runTotal) ? "matched" : "mismatch" : "unreported" },
    coverage: { reported_runs: measuredRuns, unreported_terminal_runs: usage.unreported_task_count ?? null, active_runs: runs.filter((run) => !terminal.has(run.status)).length, lower_bound: (usage.unreported_task_count ?? 0) > 0 || runs.some((run) => !terminal.has(run.status)) },
    activities: activities.map((activity) => ({ activity, attribution: activity === "other_unclassified" ? "unclassified" : "rule_estimate", tokens: measuredRuns ? buckets[activity] : missing(), share_percent: denominator > 0 ? 100 * sum(buckets[activity]) / denominator : null })),
    runs: reports.sort((a, b) => sum(b.tokens) - sum(a.tokens)),
  };
}

export function renderTable(report) {
  const labels = ["개발", "계획", "에이전트 소통", "CI 확인", "기타/분류 불가"];
  const format = (value) => value === null ? "미보고" : value.toLocaleString("en-US");
  const lines = [`${report.issue.identifier} · 해당 이슈만 · 활동은 규칙 추정`, "활동\t입력(캐시 제외)\t출력\t캐시 읽기\t캐시 쓰기\t비중"];
  report.activities.forEach((row, i) => lines.push([labels[i], ...fields.map((field) => format(row.tokens[field])), row.share_percent === null ? "—" : `${row.share_percent.toFixed(2)}%`].join("\t")));
  lines.push(`원천 합계\t${fields.map((field) => format(report.source_totals[field])).join("\t")}`);
  lines.push(`대조: 원천/실행 ${report.reconciliation.source_vs_runs}, 활동/실행 ${report.reconciliation.activities_vs_runs}`);
  lines.push(`미보고 종료 실행 ${format(report.coverage.unreported_terminal_runs)}, 실행/대기 ${report.coverage.active_runs}${report.coverage.lower_bound ? "; 합계는 현재까지 보고된 하한" : ""}`);
  lines.push("비중 분모: 보고된 입력+출력+캐시 읽기+쓰기. 비용 비중이 아님. 혼합 호출·기록 없음은 분류 불가.");
  lines.push("큰 사용 실행 (상위 5개):");
  for (const run of report.runs.slice(0, 5)) lines.push(`${run.run_id}\t${run.status}\t${run.coverage === "reported" ? format(sum(run.tokens)) : "미보고"}\t${run.models.map((row) => `${row.model}:${row.reconciliation}`).join(", ")}`);
  return lines.join("\n");
}

async function main() {
  const { values, positionals } = parseArgs({ options: { "local-rollouts": { type: "boolean" }, rollout: { type: "string", multiple: true }, output: { type: "string", default: "table" }, help: { type: "boolean" } }, allowPositionals: true });
  if (values.help) {
    console.log("node scripts/issue-activity-usage.mjs <issue> [--local-rollouts] [--rollout <run-id>=<path>] [--output table|json]\nReads existing Multica usage; local Codex logs are opt-in. No server writes. See scripts/issue-activity-usage.md.");
    return;
  }
  if (positionals.length !== 1 || !["json", "table"].includes(values.output)) throw new Error("Expected one issue and --output table|json; use --help");
  const exec = promisify(execFile);
  const query = async (...args) => {
    const { stdout } = await exec(process.env.MULTICA_CLI_PATH ?? "multica", [...args, "--output", "json"], { maxBuffer: 32 * 1024 * 1024 });
    return JSON.parse(stdout);
  };
  const issue = await query("issue", "get", positionals[0]);
  const [usage, runs] = await Promise.all([query("issue", "usage", issue.id), query("issue", "runs", issue.id)]);
  const paths = new Map();
  if (values["local-rollouts"]) {
    for (const run of runs) {
      const sessionID = run.result?.session_id;
      // Restrict discovery to daemon-owned task-local homes, never the host Owner's home.
      if (!run.work_dir || !sessionID || !/^[a-f0-9-]+$/i.test(sessionID) || !/(^|\/)workdir$/.test(run.relative_work_dir?.replaceAll("\\", "/") ?? "")) continue;
      const home = join(dirname(run.work_dir), "codex-home", "sessions");
      const matches = [];
      for await (const path of glob(`**/*-${sessionID}.jsonl`, { cwd: home })) matches.push(join(home, path));
      if (matches.length > 1) throw new Error(`Multiple rollouts for run ${run.id}; select one with --rollout`);
      if (matches.length === 1) paths.set(run.id, matches[0]);
    }
  }
  for (const entry of values.rollout ?? []) {
    const split = entry.indexOf("=");
    if (split < 1 || split === entry.length - 1) throw new Error("--rollout requires <run-id>=<path>");
    const runID = entry.slice(0, split);
    if (paths.has(runID) && !values["local-rollouts"]) throw new Error(`Duplicate rollout for run ${runID}`);
    paths.set(runID, resolve(entry.slice(split + 1)));
  }
  const report = await buildReport(issue, usage, runs, paths);
  console.log(values.output === "json" ? JSON.stringify(report, null, 2) : renderTable(report));
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => { console.error(error.message); process.exitCode = 1; });

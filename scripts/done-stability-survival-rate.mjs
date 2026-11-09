#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const INCLUDE_TERMS = [
  "修复",
  "迁移",
  "回归",
  "复活",
  "故障",
  "报错",
  "失败",
  "异常",
  "fix",
  "bug",
  "regression",
  "repair",
  "migration",
  "migrate",
  "ENOENT",
  "crash",
  "failure",
  "failed",
  "error",
];

const EXCLUDE_PATTERNS = [
  /艾伦/i,
  /closing/i,
  /close-?out/i,
  /HTML\s*化/i,
  /飞书通知/i,
  /daily\s+(brief|monitor|report)/i,
  /Loop\s*设计/i,
  /A2A同步/i,
  /架构审计/,
  /每日简报/,
  /夜间自纠错/,
  /\bSOP\b/i,
  /标准化/,
  /沉淀/,
  /业务\s*Loop/,
  /设计/,
  /审计/,
  /复核/,
  /冒烟/,
  /联调/,
  /货盘总结/,
  /CLI\s*普及/i,
  /本机装/,
  /复刻/,
  /安装/,
  /smoke\s*test/i,
  /ping\s*smoke/i,
  /日报/,
  /报告/,
];

const COMMAND_START_PATTERNS = [
  /^multica\s+/,
  /^launchctl\s+/,
  /^ps\s+/,
  /^pgrep\s+/,
  /^rg\s+/,
  /^grep\s+/,
  /^sed\s+/,
  /^awk\s+/,
  /^cat\s+/,
  /^head\s+/,
  /^tail\s+/,
  /^ls\s*(?:\s|$)/,
  /^find\s+/,
  /^stat\s+/,
  /^wc\s+/,
  /^pwd\s*$/,
  /^date\s*(?:\s|$)/,
  /^git\s+/,
  /^go\s+test\b/,
  /^node\s+(?:--version|-v|--test\b)/,
  /^pnpm\s+(?:--version|-v|test\b|exec\s+vitest\b)/,
  /^npm\s+(?:--version|-v|test\b)/,
  /^bash\s+scripts\/[A-Za-z0-9._/-]+(?:\s|$)/,
];

const ALLOWED_SEGMENT_PATTERNS = [
  /^multica\s+issue\s+get\s+\S+/,
  /^multica\s+issue\s+(?:list|search|children|pull-requests|runs|run-messages|usage)\b/,
  /^multica\s+issue\s+comment\s+list\b/,
  /^multica\s+issue\s+metadata\s+list\b/,
  /^multica\s+(?:agent|project|workspace|runtime|daemon|config|version)\b/,
  /^multica\s+attachment\s+(?:list|get)\b/,
  /^launchctl\s+(?:print|list)\b/,
  /^ps\s+/,
  /^pgrep\s+/,
  /^rg\s+/,
  /^grep\s+/,
  /^sed\s+(?!.*\s-i\b)/,
  /^awk\s+/,
  /^cat\s+/,
  /^head\s+/,
  /^tail\s+/,
  /^ls\s*(?:\s|$)/,
  /^find\s+/,
  /^stat\s+/,
  /^wc\s+/,
  /^pwd\s*$/,
  /^date\s*(?:\s|$)/,
  /^git\s+(?:status|show|log|rev-parse|branch|diff)\b/,
  /^go\s+test\b/,
  /^node\s+(?:--version|-v|--test\b)/,
  /^pnpm\s+(?:--version|-v|test\b|exec\s+vitest\b)/,
  /^npm\s+(?:--version|-v|test\b)/,
  /^bash\s+scripts\/[A-Za-z0-9._/-]+(?:\s|$)/,
  /^(?:true|false)\b/,
];

const FORBIDDEN_PATTERNS = [
  /\bsudo\b/,
  /\brm\b/,
  /\bmv\b/,
  /\bcp\b/,
  /\bkill(?:all)?\b/,
  /\bpkill\b/,
  /\bchmod\b/,
  /\bchown\b/,
  /\bmkdir\b/,
  /\btouch\b/,
  /\btee\b/,
  /\bdd\b/,
  /\b(?:curl|http|wget)\b.*\b(?:POST|PUT|PATCH|DELETE)\b/i,
  /\bgit\s+(?:reset|checkout|clean|push|commit|merge|rebase|switch|restore)\b/,
  /\bmultica\s+issue\s+(?:create|update|status|assign|rerun|cancel-task)\b/,
  /\bmultica\s+issue\s+comment\s+(?:add|delete|resolve|unresolve)\b/,
  /\bmultica\s+issue\s+metadata\s+(?:set|delete)\b/,
  /\blaunchctl\s+(?:bootout|bootstrap|kickstart|enable|disable|remove|load|unload)\b/,
];

export function classifyIssue(issue) {
  const title = String(issue?.title ?? "");
  const description = String(issue?.description ?? "");
  const haystack = `${title}\n${description}`;

  const excludedBy = EXCLUDE_PATTERNS.find((pattern) => pattern.test(haystack));
  if (excludedBy) {
    return {
      isTarget: false,
      matchedTerms: [],
      reason: `excluded by ${excludedBy}`,
    };
  }

  const lower = haystack.toLowerCase();
  const matchedTerms = INCLUDE_TERMS.filter((term) => lower.includes(term.toLowerCase()));

  return {
    isTarget: matchedTerms.length > 0,
    matchedTerms,
    reason: matchedTerms.length > 0 ? "matched fix/migration terms" : "no fix/migration terms",
  };
}

export function extractRecheckCommands(markdown) {
  const text = String(markdown ?? "");
  const commands = [];
  const fencedRanges = [];
  const fencePattern = /```([^\n`]*)\n([\s\S]*?)```/g;
  let match;

  while ((match = fencePattern.exec(text)) !== null) {
    fencedRanges.push([match.index, match.index + match[0].length]);
    const lang = match[1].trim().toLowerCase();
    if (lang && !["bash", "sh", "shell", "zsh", "console", "text", "txt"].includes(lang)) {
      continue;
    }
    for (const line of match[2].split(/\r?\n/)) {
      addCommand(commands, line);
    }
  }

  const outside = text
    .split(/\r?\n/)
    .filter((_, index, lines) => {
      const offset = lines.slice(0, index).join("\n").length + (index === 0 ? 0 : 1);
      return !fencedRanges.some(([start, end]) => offset >= start && offset < end);
    })
    .join("\n");

  for (const line of outside.split(/\r?\n/)) {
    addCommand(commands, line);
  }

  const inlinePattern = /`([^`\n]+)`/g;
  while ((match = inlinePattern.exec(outside)) !== null) {
    addCommand(commands, match[1]);
  }

  return [...new Set(commands)];
}

function addCommand(commands, rawLine) {
  let line = String(rawLine ?? "").trim();
  line = line.replace(/^\d+\.\s+/, "");
  line = line.replace(/^[-*]\s+/, "");
  line = line.replace(/^\$\s+/, "");
  line = line.replace(/^>\s+/, "");
  line = line.trim();

  if (!line || line.startsWith("#") || line.length > 500) return;
  if (/^[{\[]/.test(line)) return;
  if (!COMMAND_START_PATTERNS.some((pattern) => pattern.test(line))) return;

  commands.push(line);
}

export function isSafeRecheckCommand(command) {
  const raw = String(command ?? "").trim();
  if (!raw) return { safe: false, reason: "empty command" };
  if (raw.includes("...") || raw.includes("…")) {
    return { safe: false, reason: "placeholder ellipsis not allowed" };
  }
  if (/^multica\s+issue\s+\S*\/\S*/.test(raw)) {
    return { safe: false, reason: "compound placeholder multica command not allowed" };
  }
  if (/[`;()]/.test(raw) || raw.includes("$(")) {
    return { safe: false, reason: "shell metacharacter not allowed" };
  }
  if (/(^|[^|])>/.test(raw.replace(/\s+[12]?>\s*\/dev\/null/g, ""))) {
    return { safe: false, reason: "file redirection not allowed" };
  }

  for (const pattern of FORBIDDEN_PATTERNS) {
    if (pattern.test(raw)) {
      return { safe: false, reason: `forbidden pattern ${pattern}` };
    }
  }

  const segments = raw
    .replace(/\s+[12]?>\s*\/dev\/null/g, "")
    .split(/\s*(?:&&|\|\||\|)\s*/)
    .map((segment) => segment.replace(/^!\s*/, "").trim())
    .filter(Boolean);

  if (segments.length === 0) return { safe: false, reason: "empty command" };

  for (const segment of segments) {
    if (/^ps\s+[A-Za-z_]+$/.test(segment) && !/^ps\s+aux/.test(segment)) {
      return { safe: false, reason: `incomplete ps command: ${segment}` };
    }
    if (!ALLOWED_SEGMENT_PATTERNS.some((pattern) => pattern.test(segment))) {
      return { safe: false, reason: `unsupported command segment: ${segment}` };
    }
  }

  return { safe: true, reason: "read-only allowlist" };
}

export function normalizeIssueRecord(issue) {
  return {
    id: String(issue.id ?? ""),
    identifier: String(issue.identifier ?? ""),
    title: String(issue.title ?? ""),
    description: String(issue.description ?? ""),
    status: String(issue.status ?? ""),
    updated_at: String(issue.updated_at ?? ""),
    done_at: String(issue.done_at ?? issue.updated_at ?? ""),
    done_at_source: issue.done_at ? "done_at" : "updated_at",
  };
}

export function normalizeReportRow(row) {
  if (row.status === "pass") return { status: "pass", pass: true };
  if (row.status === "fail") return { status: "fail", pass: false };
  if (row.status === "skipped") return { status: "skipped", pass: null };
  if (row.pass === true) return { status: "pass", pass: true };
  if (row.pass === false) return { status: "fail", pass: false };
  return { status: "skipped", pass: null };
}

export function summarizeReport(rows) {
  const normalized = rows.map(normalizeReportRow);
  const total = normalized.length;
  const skipped = normalized.filter((row) => row.status === "skipped").length;
  const evaluated = total - skipped;
  const pass = normalized.filter((row) => row.status === "pass").length;
  const fail = normalized.filter((row) => row.status === "fail").length;
  return {
    total,
    evaluated,
    skipped,
    pass,
    fail,
    survival_rate: evaluated === 0 ? null : Number((pass / evaluated).toFixed(4)),
  };
}

export function parseArgs(argv) {
  const now = new Date();
  const options = {
    days: 7,
    pageSize: 100,
    commentThreads: 10,
    timeoutMs: 30_000,
    output: defaultReportPath(now),
    outputDir: defaultReportDir(),
    dryRun: false,
    maxRechecks: Infinity,
    installLaunchd: false,
    loadLaunchd: false,
    launchdHour: 9,
    launchdMinute: 15,
    launchdLabel: "ai.multica.done-stability-survival-rate",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = () => {
      i += 1;
      if (i >= argv.length) throw new Error(`${arg} requires a value`);
      return argv[i];
    };

    if (arg === "--help" || arg === "-h") {
      return { ...options, help: true };
    }
    if (arg === "--output") options.output = resolve(process.cwd(), next());
    else if (arg === "--output-dir") {
      options.outputDir = resolve(process.cwd(), next());
      options.output = reportPathForDir(options.outputDir, now);
    }
    else if (arg === "--days") options.days = Number(next());
    else if (arg === "--page-size") options.pageSize = Number(next());
    else if (arg === "--comment-threads") options.commentThreads = Number(next());
    else if (arg === "--timeout-ms") options.timeoutMs = Number(next());
    else if (arg === "--max-rechecks") options.maxRechecks = Number(next());
    else if (arg === "--dry-run") options.dryRun = true;
    else if (arg === "--install-launchd") options.installLaunchd = true;
    else if (arg === "--load-launchd") {
      options.installLaunchd = true;
      options.loadLaunchd = true;
    }
    else if (arg === "--launchd-hour") options.launchdHour = Number(next());
    else if (arg === "--launchd-minute") options.launchdMinute = Number(next());
    else if (arg === "--launchd-label") options.launchdLabel = next();
    else throw new Error(`unknown argument: ${arg}`);
  }

  if (!Number.isFinite(options.days) || options.days < 0) throw new Error("--days must be >= 0");
  if (!Number.isInteger(options.pageSize) || options.pageSize <= 0) throw new Error("--page-size must be > 0");
  if (!Number.isInteger(options.commentThreads) || options.commentThreads < 0) {
    throw new Error("--comment-threads must be >= 0");
  }
  if (!Number.isInteger(options.timeoutMs) || options.timeoutMs <= 0) throw new Error("--timeout-ms must be > 0");
  if (options.maxRechecks !== Infinity && (!Number.isFinite(options.maxRechecks) || options.maxRechecks < 0)) {
    throw new Error("--max-rechecks must be >= 0");
  }
  if (!Number.isInteger(options.launchdHour) || options.launchdHour < 0 || options.launchdHour > 23) {
    throw new Error("--launchd-hour must be between 0 and 23");
  }
  if (!Number.isInteger(options.launchdMinute) || options.launchdMinute < 0 || options.launchdMinute > 59) {
    throw new Error("--launchd-minute must be between 0 and 59");
  }

  return options;
}

export function defaultReportDir() {
  return resolve(homedir(), ".multica", "reports", "done-stability-survival-rate");
}

export function defaultReportPath(date = new Date()) {
  return reportPathForDir(defaultReportDir(), date);
}

function reportPathForDir(outputDir, date = new Date()) {
  const stamp = date.toISOString().slice(0, 10);
  return resolve(outputDir, `done-stability-survival-rate-${stamp}.json`);
}

function printHelp() {
  console.log(`Usage: node scripts/done-stability-survival-rate.mjs [options]

Options:
  --output <path>          JSON report path (overrides --output-dir)
  --output-dir <path>      Retained JSON report directory (default: ~/.multica/reports/done-stability-survival-rate)
  --days <n>               Stable age threshold in days, using issue.updated_at fallback (default: 7)
  --page-size <n>          multica issue list page size (default: 100)
  --comment-threads <n>    recent comment threads to scan per candidate (default: 10)
  --timeout-ms <n>         per-command timeout (default: 30000)
  --max-rechecks <n>       cap evaluated target issues after filtering (default: unlimited)
  --dry-run                do not execute recheck commands; report what would run
  --install-launchd        Install daily macOS LaunchAgent for retained reports
  --load-launchd           Install and bootstrap the LaunchAgent immediately
  --launchd-hour <0-23>    LaunchAgent daily hour (default: 9)
  --launchd-minute <0-59>  LaunchAgent daily minute (default: 15)
`);
}

export function buildLaunchdPlist({
  label,
  nodePath,
  scriptPath,
  outputDir,
  logDir,
  hour,
  minute,
  environment = {},
}) {
  const args = [nodePath, scriptPath, "--output-dir", outputDir];
  const environmentEntries = Object.entries(environment)
    .filter(([key, value]) => key && typeof value === "string")
    .map(([key, value]) => `    <key>${escapeXml(key)}</key>\n    <string>${escapeXml(value)}</string>`)
    .join("\n");
  const environmentBlock = environmentEntries
    ? `  <key>EnvironmentVariables</key>\n  <dict>\n${environmentEntries}\n  </dict>\n`
    : "";
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${escapeXml(label)}</string>
  <key>ProgramArguments</key>
  <array>
${args.map((arg) => `    <string>${escapeXml(arg)}</string>`).join("\n")}
  </array>
  <key>WorkingDirectory</key>
  <string>${escapeXml(repoRoot)}</string>
${environmentBlock}  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key>
    <integer>${hour}</integer>
    <key>Minute</key>
    <integer>${minute}</integer>
  </dict>
  <key>StandardOutPath</key>
  <string>${escapeXml(resolve(logDir, `${label}.out.log`))}</string>
  <key>StandardErrorPath</key>
  <string>${escapeXml(resolve(logDir, `${label}.err.log`))}</string>
</dict>
</plist>
`;
}

function escapeXml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

async function installLaunchd(options) {
  const launchAgentsDir = resolve(homedir(), "Library", "LaunchAgents");
  const logDir = resolve(homedir(), ".multica", "logs");
  const plistPath = resolve(launchAgentsDir, `${options.launchdLabel}.plist`);
  const scriptPath = resolve(repoRoot, "scripts", "done-stability-survival-rate.mjs");
  mkdirSync(launchAgentsDir, { recursive: true });
  mkdirSync(logDir, { recursive: true });
  mkdirSync(options.outputDir, { recursive: true });
  const plist = buildLaunchdPlist({
    label: options.launchdLabel,
    nodePath: process.execPath,
    scriptPath,
    outputDir: options.outputDir,
    logDir,
    hour: options.launchdHour,
    minute: options.launchdMinute,
    environment: defaultLaunchdEnvironment(),
  });
  writeFileSync(plistPath, plist);
  console.log(plistPath);

  if (options.loadLaunchd) {
    const uid = process.getuid?.();
    if (!Number.isInteger(uid)) throw new Error("--load-launchd requires a numeric uid");
    const cleanEnv = { PATH: "/usr/bin:/bin:/usr/sbin:/sbin" };
    await runProcess("launchctl", ["bootout", `gui/${uid}`, plistPath], { timeoutMs: 10_000, env: cleanEnv });
    const loaded = await runProcess("launchctl", ["bootstrap", `gui/${uid}`, plistPath], {
      timeoutMs: 10_000,
      env: cleanEnv,
    });
    if (loaded.exitCode !== 0) {
      throw new Error(`launchctl bootstrap failed: ${loaded.stderrTail || loaded.stdoutTail}`);
    }
  }
}

function defaultLaunchdEnvironment() {
  const pathEntries = [
    dirname(process.execPath),
    resolve(homedir(), ".local", "bin"),
    "/opt/homebrew/bin",
    "/usr/local/bin",
    "/usr/bin",
    "/bin",
    "/usr/sbin",
    "/sbin",
  ];
  return {
    PATH: [...new Set(pathEntries)].join(":"),
    OPENAI_API_KEY: "",
    OPENAI_BASE_URL: "",
    MULTICA_SKILL_TRACE_ENABLED: "",
    MULTICA_SKILL_TRACE_PATH: "",
  };
}

export function isRetryableMulticaError(message) {
  return /context deadline exceeded|Client\.Timeout|timeout|temporar|ECONNRESET|EOF/i.test(String(message ?? ""));
}

async function runMulticaJSON(args, { attempts = 3 } = {}) {
  let lastError = null;

  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    const result = await runProcess("multica", args, { timeoutMs: 60_000 });
    if (result.exitCode !== 0) {
      const message = `multica ${args.join(" ")} failed: ${result.stderrTail || result.stdoutTail}`;
      lastError = new Error(message);
      if (attempt < attempts && isRetryableMulticaError(message)) {
        console.error(`retrying ${args.join(" ")} after transient error (${attempt}/${attempts})`);
        await sleep(750 * attempt);
        continue;
      }
      throw lastError;
    }
    try {
      return JSON.parse(result.stdout);
    } catch (error) {
      throw new Error(`failed to parse multica JSON for ${args.join(" ")}: ${error.message}`);
    }
  }

  throw lastError ?? new Error(`multica ${args.join(" ")} failed`);
}

function sleep(ms) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, ms));
}

async function listDoneIssues(pageSize) {
  const issues = [];
  let total = null;
  let offset = 0;

  do {
    const page = await runMulticaJSON([
      "issue",
      "list",
      "--status",
      "done",
      "--limit",
      String(pageSize),
      "--offset",
      String(offset),
      "--output",
      "json",
    ]);

    if (!Number.isInteger(page.total)) {
      throw new Error("multica issue list JSON did not include integer total");
    }
    total = page.total;
    const pageIssues = Array.isArray(page.issues) ? page.issues : [];
    issues.push(...pageIssues);
    offset += pageIssues.length;

    if (pageIssues.length === 0) break;
  } while (offset < total);

  return { issues, total: total ?? 0 };
}

async function fetchIssueSurface(issueID, commentThreads) {
  if (commentThreads === 0) return { comments: [], comment_error: null };
  try {
    const comments = await runMulticaJSON([
      "issue",
      "comment",
      "list",
      issueID,
      "--recent",
      String(commentThreads),
      "--output",
      "json",
    ]);
    return { comments: Array.isArray(comments) ? comments : [], comment_error: null };
  } catch (error) {
    return { comments: [], comment_error: error.message };
  }
}

function commandScore(command) {
  const c = command.toLowerCase();
  if (/pgrep|ps\s|launchctl|daemon\s+status|runtime/.test(c)) return 100;
  if (/multica\s+issue\s+(?:get|runs|comment\s+list)/.test(c)) return 80;
  if (/go\s+test|pnpm\s+test|npm\s+test|node\s+--test/.test(c)) return 70;
  if (/rg|grep|wc|find|ls|stat/.test(c)) return 60;
  return 10;
}

export function pickRecheckCommand(commands, context = "") {
  const safeCommands = commands
    .map((command) => ({
      command,
      safety: isSafeRecheckCommand(command),
      assertion: inferSemanticAssertion(command, context),
    }))
    .filter((entry) => entry.safety.safe)
    .sort((a, b) => commandScore(b.command) - commandScore(a.command));
  return (
    safeCommands.find((entry) => entry.assertion.kind !== "unsupported")?.command
    ?? safeCommands[0]?.command
    ?? ""
  );
}

export function inferSemanticAssertion(command, context) {
  const raw = String(command ?? "").trim();
  const surrounding = String(context ?? "").toLowerCase();
  const grepPattern = extractPipelineSearchPattern(raw);
  if (grepPattern) {
    return {
      kind: "stdout_matches",
      pattern: grepPattern,
      reason: "pipeline grep/rg pattern must appear in stdout",
    };
  }

  const standaloneSearchPattern = extractStandaloneSearchPattern(raw);
  if (standaloneSearchPattern) {
    return {
      kind: "stdout_matches",
      pattern: standaloneSearchPattern,
      reason: "standalone grep/rg pattern must appear in stdout",
    };
  }

  const pgrepPattern = extractPgrepPattern(raw);
  if (pgrepPattern) {
    if (/无进程|不存在|不应存在|not running|no process|absent|gone/.test(surrounding)) {
      return {
        kind: "stdout_absent",
        pattern: pgrepPattern,
        reason: "concrete process pattern must be absent",
      };
    }
    return {
      kind: "stdout_matches",
      pattern: pgrepPattern,
      reason: "concrete process pattern must appear in stdout",
    };
  }

  return {
    kind: "unsupported",
    reason: "no semantic assertion can be inferred",
  };
}

function extractPipelineSearchPattern(command) {
  const segments = String(command ?? "")
    .split(/\s*\|\s*/)
    .map((segment) => segment.trim());
  for (const segment of segments.slice(1)) {
    if (/^(?:rg|grep)\b/.test(segment)) {
      return firstSearchArgument(segment);
    }
  }
  return "";
}

function extractPgrepPattern(command) {
  const trimmed = String(command ?? "").trim();
  if (!/^pgrep\b/.test(trimmed)) return "";
  return firstSearchArgument(trimmed);
}

function extractStandaloneSearchPattern(command) {
  const trimmed = String(command ?? "").trim();
  if (!/^(?:rg|grep)\b/.test(trimmed)) return "";
  return firstSearchArgument(trimmed);
}

function firstSearchArgument(segment) {
  const tokens = shellWords(segment);
  for (const token of tokens.slice(1)) {
    if (!token || token.startsWith("-")) continue;
    return token;
  }
  return "";
}

function shellWords(input) {
  const words = [];
  const re = /"([^"]*)"|'([^']*)'|(\S+)/g;
  let match;
  while ((match = re.exec(String(input ?? ""))) !== null) {
    words.push(match[1] ?? match[2] ?? match[3] ?? "");
  }
  return words;
}

export function evaluateCommandResult({ command, assertion, result }) {
  const actualAssertion = assertion ?? inferSemanticAssertion(command, "");
  if (!actualAssertion || actualAssertion.kind === "unsupported") {
    return {
      status: "skipped",
      pass: null,
      reason: actualAssertion?.reason ?? "no semantic assertion can be inferred",
    };
  }

  const output = `${result?.stdoutTail ?? ""}\n${result?.stderrTail ?? ""}`;
  if (result?.timedOut) {
    return { status: "fail", pass: false, reason: "command timed out" };
  }

  if (actualAssertion.kind === "stdout_matches") {
    const matched = output.includes(actualAssertion.pattern);
    return {
      status: result?.exitCode === 0 && matched ? "pass" : "fail",
      pass: result?.exitCode === 0 && matched,
      reason: matched
        ? `matched required output: ${actualAssertion.pattern}`
        : `missing required output: ${actualAssertion.pattern}`,
    };
  }

  if (actualAssertion.kind === "stdout_absent") {
    const matched = output.includes(actualAssertion.pattern);
    return {
      status: result?.exitCode === 0 && !matched ? "pass" : "fail",
      pass: result?.exitCode === 0 && !matched,
      reason: matched
        ? `forbidden output was present: ${actualAssertion.pattern}`
        : `forbidden output absent: ${actualAssertion.pattern}`,
    };
  }

  return { status: "skipped", pass: null, reason: "unsupported semantic assertion" };
}

async function evaluateIssue(rawIssue, options) {
  const issue = normalizeIssueRecord(rawIssue);
  const surface = await fetchIssueSurface(issue.id, options.commentThreads);
  const commentText = surface.comments.map((comment) => String(comment.content ?? "")).join("\n\n");
  const context = `${issue.title}\n\n${issue.description}\n\n${commentText}`;
  const commands = extractRecheckCommands(context);
  const recheckCmd = pickRecheckCommand(commands, context);

  if (!recheckCmd) {
    return {
      issue: {
        id: issue.id,
        identifier: issue.identifier,
        title: issue.title,
      },
      done_at: issue.done_at,
      done_at_source: issue.done_at_source,
      recheck_cmd: "",
      status: "skipped",
      pass: null,
      evidence: {
        reason: surface.comment_error
          ? `no safe recheck command found; comment read failed: ${surface.comment_error}`
          : "no safe recheck command found in issue description or recent comments",
        extracted_commands: commands,
      },
    };
  }

  const assertion = inferSemanticAssertion(recheckCmd, `${issue.title}\n\n${issue.description}\n\n${commentText}`);
  if (assertion.kind === "unsupported") {
    return {
      issue: {
        id: issue.id,
        identifier: issue.identifier,
        title: issue.title,
      },
      done_at: issue.done_at,
      done_at_source: issue.done_at_source,
      recheck_cmd: recheckCmd,
      semantic_assertion: assertion,
      status: "skipped",
      pass: null,
      evidence: {
        reason: assertion.reason,
      },
    };
  }

  if (options.dryRun) {
    return {
      issue: {
        id: issue.id,
        identifier: issue.identifier,
        title: issue.title,
      },
      done_at: issue.done_at,
      done_at_source: issue.done_at_source,
      recheck_cmd: recheckCmd,
      semantic_assertion: assertion,
      status: "skipped",
      pass: null,
      evidence: {
        reason: "dry run; command was not executed",
      },
    };
  }

  const startedAt = Date.now();
  const result = await runProcess("/bin/zsh", ["-lc", recheckCmd], {
    timeoutMs: options.timeoutMs,
  });
  const semantic = evaluateCommandResult({ command: recheckCmd, assertion, result });
  return {
    issue: {
      id: issue.id,
      identifier: issue.identifier,
      title: issue.title,
    },
    done_at: issue.done_at,
    done_at_source: issue.done_at_source,
    recheck_cmd: recheckCmd,
    semantic_assertion: assertion,
    status: semantic.status,
    pass: semantic.pass,
    evidence: {
      assertion_result: semantic.reason,
      exit_code: result.exitCode,
      timed_out: result.timedOut,
      duration_ms: Date.now() - startedAt,
      stdout_tail: result.stdoutTail,
      stderr_tail: result.stderrTail,
    },
  };
}

async function runProcess(command, args, { timeoutMs, env = process.env }) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, {
      cwd: process.cwd(),
      env,
      stdio: ["ignore", "pipe", "pipe"],
    });

    let stdout = "";
    let stderr = "";
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGTERM");
      setTimeout(() => child.kill("SIGKILL"), 2_000).unref();
    }, timeoutMs);

    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString("utf8");
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", (error) => {
      clearTimeout(timer);
      resolvePromise({
        exitCode: 127,
        timedOut,
        stdout,
        stderr: `${stderr}${error.message}`,
        stdoutTail: tail(stdout),
        stderrTail: tail(`${stderr}${error.message}`),
      });
    });
    child.on("close", (code, signal) => {
      clearTimeout(timer);
      resolvePromise({
        exitCode: timedOut ? 124 : (code ?? 1),
        signal,
        timedOut,
        stdout,
        stderr,
        stdoutTail: tail(stdout),
        stderrTail: tail(stderr),
      });
    });
  });
}

function tail(text, max = 4000) {
  const value = String(text ?? "").trim();
  return value.length <= max ? value : value.slice(value.length - max);
}

function isOlderThanCutoff(issue, cutoffMs) {
  const doneAt = Date.parse(issue.done_at);
  return Number.isFinite(doneAt) && doneAt <= cutoffMs;
}

async function buildReport(options) {
  const generatedAt = new Date();
  const cutoffMs = generatedAt.getTime() - options.days * 24 * 60 * 60 * 1000;
  const { issues: rawDoneIssues, total: doneTotal } = await listDoneIssues(options.pageSize);

  const candidates = rawDoneIssues
    .map((issue) => ({ issue: normalizeIssueRecord(issue), classification: classifyIssue(issue) }))
    .filter(({ issue, classification }) => classification.isTarget && isOlderThanCutoff(issue, cutoffMs))
    .slice(0, options.maxRechecks);

  const rows = [];
  for (const { issue, classification } of candidates) {
    const row = await evaluateIssue(issue, options);
    row.classification = classification;
    rows.push(row);
    console.error(
      `[${rows.length}/${candidates.length}] ${row.issue.identifier || row.issue.id}: ${row.status}`,
    );
  }

  return {
    generated_at: generatedAt.toISOString(),
    cutoff_days: options.days,
    cutoff_at: new Date(cutoffMs).toISOString(),
    source: {
      issue_query: "multica issue list --status done --output json",
      count_method: "paginated by JSON total field",
      done_total: doneTotal,
      done_rows_read: rawDoneIssues.length,
      done_at_note:
        "The current multica CLI does not expose a dedicated done_at field; report rows use issue.updated_at as a conservative stability timestamp.",
    },
    summary: summarizeReport(rows),
    issues: rows,
  };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    printHelp();
    return;
  }
  if (options.installLaunchd) {
    await installLaunchd(options);
    return;
  }

  const report = await buildReport(options);
  mkdirSync(dirname(options.output), { recursive: true });
  writeFileSync(options.output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(options.output);
  console.log(
    `done_total=${report.source.done_total} total=${report.summary.total} evaluated=${report.summary.evaluated} skipped=${report.summary.skipped} pass=${report.summary.pass} fail=${report.summary.fail} survival_rate=${report.summary.survival_rate}`,
  );
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error.stack || error.message);
    process.exit(1);
  });
}

#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
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

export function summarizeReport(rows) {
  const total = rows.length;
  const pass = rows.filter((row) => row.pass === true).length;
  const fail = total - pass;
  return {
    total,
    pass,
    fail,
    survival_rate: total === 0 ? null : Number((pass / total).toFixed(4)),
  };
}

export function parseArgs(argv) {
  const stamp = new Date().toISOString().slice(0, 10);
  const options = {
    days: 7,
    pageSize: 100,
    commentThreads: 10,
    timeoutMs: 30_000,
    output: resolve(repoRoot, "artifacts", `done-stability-survival-rate-${stamp}.json`),
    dryRun: false,
    maxRechecks: Infinity,
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
    else if (arg === "--days") options.days = Number(next());
    else if (arg === "--page-size") options.pageSize = Number(next());
    else if (arg === "--comment-threads") options.commentThreads = Number(next());
    else if (arg === "--timeout-ms") options.timeoutMs = Number(next());
    else if (arg === "--max-rechecks") options.maxRechecks = Number(next());
    else if (arg === "--dry-run") options.dryRun = true;
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

  return options;
}

function printHelp() {
  console.log(`Usage: node scripts/done-stability-survival-rate.mjs [options]

Options:
  --output <path>          JSON report path (default: artifacts/done-stability-survival-rate-YYYY-MM-DD.json)
  --days <n>               Stable age threshold in days, using issue.updated_at fallback (default: 7)
  --page-size <n>          multica issue list page size (default: 100)
  --comment-threads <n>    recent comment threads to scan per candidate (default: 10)
  --timeout-ms <n>         per-command timeout (default: 30000)
  --max-rechecks <n>       cap evaluated target issues after filtering (default: unlimited)
  --dry-run                do not execute recheck commands; report what would run
`);
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

function pickRecheckCommand(commands) {
  const safeCommands = commands
    .map((command) => ({ command, safety: isSafeRecheckCommand(command) }))
    .filter((entry) => entry.safety.safe)
    .sort((a, b) => commandScore(b.command) - commandScore(a.command));
  return safeCommands[0]?.command ?? "";
}

async function evaluateIssue(rawIssue, options) {
  const issue = normalizeIssueRecord(rawIssue);
  const surface = await fetchIssueSurface(issue.id, options.commentThreads);
  const commentText = surface.comments.map((comment) => String(comment.content ?? "")).join("\n\n");
  const commands = extractRecheckCommands(`${issue.title}\n\n${issue.description}\n\n${commentText}`);
  const recheckCmd = pickRecheckCommand(commands);

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
      pass: false,
      evidence: {
        reason: surface.comment_error
          ? `no safe recheck command found; comment read failed: ${surface.comment_error}`
          : "no safe recheck command found in issue description or recent comments",
        extracted_commands: commands,
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
      pass: true,
      evidence: {
        reason: "dry run; command was not executed",
      },
    };
  }

  const startedAt = Date.now();
  const result = await runProcess("/bin/zsh", ["-lc", recheckCmd], {
    timeoutMs: options.timeoutMs,
  });
  return {
    issue: {
      id: issue.id,
      identifier: issue.identifier,
      title: issue.title,
    },
    done_at: issue.done_at,
    done_at_source: issue.done_at_source,
    recheck_cmd: recheckCmd,
    pass: result.exitCode === 0,
    evidence: {
      exit_code: result.exitCode,
      timed_out: result.timedOut,
      duration_ms: Date.now() - startedAt,
      stdout_tail: result.stdoutTail,
      stderr_tail: result.stderrTail,
    },
  };
}

async function runProcess(command, args, { timeoutMs }) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, {
      cwd: process.cwd(),
      env: process.env,
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
      `[${rows.length}/${candidates.length}] ${row.issue.identifier || row.issue.id}: ${row.pass ? "pass" : "fail"}`,
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

  const report = await buildReport(options);
  mkdirSync(dirname(options.output), { recursive: true });
  writeFileSync(options.output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(options.output);
  console.log(
    `done_total=${report.source.done_total} evaluated=${report.summary.total} pass=${report.summary.pass} fail=${report.summary.fail} survival_rate=${report.summary.survival_rate}`,
  );
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error.stack || error.message);
    process.exit(1);
  });
}

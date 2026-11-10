import type { AgentTask, IssueEnvironment, IssueEnvironmentActionRecord } from "@multica/core/types";

const DAY = 24 * 60 * 60_000;
const RECENT_FAILURE = 10 * 60_000;

/** Every localized "let the agent start it" instruction names this command,
 *  which is how the sidebar recognizes those runs whatever the reader's language. */
export const ENVIRONMENT_START_MARKER = "multica env start";

export const isLive = (env: IssueEnvironment) => env.ended_at === null;

/** The exit code a daemon recorded as `exit status N`, if any. */
export function exitCode(env: IssueEnvironment): number | null {
  const match = /exit status (\d+)/.exec(env.detail);
  return match ? Number(match[1]) : null;
}

/** A declared environment that crashed stays in the main list for a day, or
 *  until it is restarted or another one with its name starts on the machine. */
export function isPinnedCrash(env: IssueEnvironment, all: IssueEnvironment[], now: number): boolean {
  if (env.source !== "config" || env.state !== "exited" || !env.ended_at) return false;
  const code = exitCode(env);
  if (code === 0 || (code === null && !env.excerpt)) return false;
  const ended = new Date(env.ended_at).getTime();
  if (now - ended > DAY) return false;
  return !all.some((other) =>
    other.id !== env.id &&
    other.runtime_id === env.runtime_id &&
    other.name === env.name &&
    other.source === "config" &&
    new Date(other.started_at).getTime() >= ended,
  );
}

/** The command the machine is still working on for this environment. */
export function pendingAction(env: IssueEnvironment): IssueEnvironmentActionRecord | null {
  return env.pending_action?.status === "pending" ? env.pending_action : null;
}

/** A stop or restart that failed or went unanswered in the last few minutes. */
export function recentFailure(env: IssueEnvironment, now: number): ControlRecord | null {
  const last = env.last_action;
  if (!last || last.action === "logs" || (last.status !== "failed" && last.status !== "expired")) return null;
  const at = new Date(last.completed_at ?? last.created_at).getTime();
  return now - at <= RECENT_FAILURE ? { ...last, action: last.action } : null;
}

type ControlRecord = IssueEnvironmentActionRecord & { action: "stop" | "restart" | "start" };

/** 1-based position of each run on the issue, oldest first. */
export function runOrdinals(tasks: readonly AgentTask[]): Map<string, number> {
  // Ids are time-ordered, which settles runs created in the same second.
  const ordered = [...tasks].sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id));
  return new Map(ordered.map((task, index) => [task.id, index + 1]));
}

const ACTIVE_TASK_STATUSES = new Set<AgentTask["status"]>(["queued", "dispatched", "waiting_local_directory", "deferred", "running"]);

export function isActiveTask(task: AgentTask): boolean {
  return ACTIVE_TASK_STATUSES.has(task.status);
}

/** The agent run asked to start this issue's environment that is still under way. */
export function preparingRun(tasks: readonly AgentTask[], requestedCommentIds: ReadonlySet<string>): AgentTask | null {
  return tasks.find((task) =>
    isActiveTask(task) && (
      (task.trigger_comment_id !== undefined && requestedCommentIds.has(task.trigger_comment_id)) ||
      (task.trigger_summary ?? "").includes(ENVIRONMENT_START_MARKER)
    ),
  ) ?? null;
}

export type LogLevel = "error" | "warn" | null;

/** Colors a log the way a reader scans it: errors and the lines continuing
 *  them in red, warnings in yellow. */
export function classifyLogLines(lines: readonly string[]): LogLevel[] {
  const levels: LogLevel[] = [];
  let previous: LogLevel = null;
  for (const line of lines) {
    let level: LogLevel = null;
    if (/\b(ERROR|ERR|FATAL|PANIC)\b|^(panic|fatal error|error|Error)\b|\*\*\*.*\bError\b/.test(line)) {
      level = "error";
    } else if (/\b(WARN|WARNING)\b/.test(line)) {
      level = "warn";
    } else if (previous === "error" && /^\s+\S/.test(line)) {
      level = "error";
    }
    levels.push(level);
    previous = level;
  }
  return levels;
}

export function isLoopbackUrl(url: string): boolean {
  try {
    const host = new URL(url).hostname;
    return host === "localhost" || host.endsWith(".localhost") || host === "[::1]" || host === "0.0.0.0" || host.startsWith("127.");
  } catch {
    return false;
  }
}

export function displayHost(url: string): string | null {
  try {
    const parsed = new URL(url);
    const path = parsed.pathname === "/" ? "" : parsed.pathname;
    return `${parsed.host}${path}`;
  } catch {
    return null;
  }
}

/** The environment's name as rows, menus and dialogs show it. */
export function environmentTitle(env: IssueEnvironment): string {
  const app = env.kind === "app";
  const host = !app && env.url ? displayHost(env.url) : null;
  return app || env.source === "config" ? env.label || env.name : host ?? env.name;
}

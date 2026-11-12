import type { DevRequest } from "@multica/core/diagnostics";

export type MetricTone = "normal" | "warn" | "bad";

export interface TimeSpan {
  start: number;
  end: number;
}

/** An input older than this did not cause the route change that followed it. */
export const NAV_INPUT_MAX_AGE_MS = 10_000;

export const JANK_WINDOW_MS = 10_000;

export function formatBytes(bytes: number): string {
  const mb = bytes / 1024 / 1024;
  if (mb < 1024) return `${Math.round(mb)}MB`;
  return `${(mb / 1024).toFixed(2)}GB`;
}

export function memoryTone(bytes: number): MetricTone {
  const mb = bytes / 1024 / 1024;
  if (mb >= 1024) return "bad";
  if (mb >= 500) return "warn";
  return "normal";
}

/**
 * Time from the input that triggered a route change to the frame that showed
 * it, or null when no recent input explains the change (a redirect or a
 * timer-driven navigation).
 */
export function navDuration(inputAt: number | null, paintedAt: number): number | null {
  if (inputAt === null) return null;
  const elapsed = paintedAt - inputAt;
  if (elapsed < 0 || elapsed > NAV_INPUT_MAX_AGE_MS) return null;
  return elapsed;
}

export function navTone(ms: number): MetricTone {
  if (ms >= 1000) return "bad";
  if (ms >= 300) return "warn";
  return "normal";
}

/** Share of the trailing window the main thread spent inside long tasks, 0–1. */
export function blockedRatio(
  tasks: readonly TimeSpan[],
  now: number,
  windowMs: number = JANK_WINDOW_MS,
): number {
  const windowStart = now - windowMs;
  let blocked = 0;
  for (const task of tasks) {
    const start = Math.max(task.start, windowStart);
    const end = Math.min(task.end, now);
    if (end > start) blocked += end - start;
  }
  return Math.min(1, blocked / windowMs);
}

export function jankTone(ratio: number): MetricTone {
  if (ratio >= 0.2) return "bad";
  if (ratio >= 0.05) return "warn";
  return "normal";
}

/** Host and port of an API URL, which is what tells two local backends apart. */
export function apiHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/** Ended with an error status or no response. A request we cancelled did not fail. */
export function requestFailed(request: DevRequest): boolean {
  if (request.durationMs === null) return false;
  if (request.status === null) return request.error !== "aborted";
  return request.status >= 400;
}

/** Nearest-rank percentile; null for no values. */
export function percentile(values: readonly number[], p: number): number | null {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const rank = Math.ceil((p / 100) * sorted.length);
  return sorted[Math.min(sorted.length, Math.max(1, rank)) - 1]!;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

/**
 * The request as a curl command. The token stays a shell variable so the
 * command can be pasted into an issue without leaking a credential. Null when
 * the recorded body is incomplete: a cut body would replay as invalid JSON.
 */
export function curlCommand(apiUrl: string, request: DevRequest, workspaceSlug: string | null): string | null {
  if (request.bodyNote !== null) return null;
  const parts = [`curl -X ${request.method} ${shellQuote(`${apiUrl.replace(/\/$/, "")}${request.path}`)}`];
  parts.push(`-H "Authorization: Bearer $MULTICA_TOKEN"`);
  if (workspaceSlug) parts.push(`-H ${shellQuote(`X-Workspace-Slug: ${workspaceSlug}`)}`);
  if (request.body !== null) {
    parts.push(`-H 'Content-Type: application/json'`);
    parts.push(`--data ${shellQuote(request.body)}`);
  }
  return parts.join(" \\\n  ");
}

/** The first `at …` line of a stack, which is usually where to look. */
export function firstStackFrame(stack: string | null): string | null {
  if (!stack) return null;
  const frame = stack.split("\n").find((line) => /^\s*at\s/.test(line));
  return frame ? frame.trim() : null;
}

/** Wall-clock `HH:MM:SS`, which lines up with the server's logs. */
export function formatClock(at: number): string {
  return new Date(at).toTimeString().slice(0, 8);
}

export function formatDuration(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`;
}

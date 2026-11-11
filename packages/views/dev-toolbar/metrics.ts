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

import type { RuntimeRateLimitWindow } from "../types";

// Runtime rate limits (MUL-9427): the plan windows (5-hour session, weekly)
// of the account a runtime's agent CLI runs under, as the CLI last reported
// them. These helpers turn one reported window into what the UI shows: tone,
// whether usage runs ahead of the time elapsed, and when it would run out.

/** Providers whose CLI reports rate limits that the daemon forwards. */
const RATE_LIMIT_PROVIDERS = new Set(["claude", "codex"]);

export function runtimeReportsRateLimits(provider: string): boolean {
  return RATE_LIMIT_PROVIDERS.has(provider);
}

export const RATE_LIMIT_WARNING_PERCENT = 70;
export const RATE_LIMIT_CRITICAL_PERCENT = 90;
/** Usage this many points ahead of the elapsed time counts as over pace. */
export const RATE_LIMIT_PACE_TOLERANCE = 5;
/** Below this share of the window elapsed, a pace verdict is noise. */
const MIN_ELAPSED_FOR_PACE = 0.03;

export type RateLimitTone = "normal" | "warning" | "critical";

export interface RateLimitWindowView {
  window: RuntimeRateLimitWindow;
  /** Reported share used, 0-100. Undefined when the CLI did not say. */
  used?: number;
  /** The window reset after the report, so its usage belongs to the last one. */
  hasReset: boolean;
  /** At or past the limit: the provider rejected requests or usage hit 100%. */
  limited: boolean;
  tone: RateLimitTone;
  /** Share of the window elapsed when the usage was reported, 0-100. */
  elapsedPercent?: number;
  /** Points by which usage ran ahead of the elapsed time (only when ahead). */
  aheadOfPace?: number;
  /** Minutes from now until the window runs out at the reported pace (only when ahead). */
  runsOutInMinutes?: number;
  /** Minutes from now until the window resets. */
  resetsInMinutes?: number;
}

function parseTime(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : undefined;
}

export function describeRateLimitWindow(
  window: RuntimeRateLimitWindow,
  observedAt: string,
  now: number,
): RateLimitWindowView {
  const resetMs = parseTime(window.resets_at);
  const observedMs = parseTime(observedAt) ?? now;
  const hasReset = resetMs !== undefined && resetMs <= now;
  const used = hasReset ? undefined : window.used_percent;
  const limited =
    !hasReset && (window.status === "rejected" || (used !== undefined && used >= 100));

  let tone: RateLimitTone = "normal";
  if (limited || (used !== undefined && used >= RATE_LIMIT_CRITICAL_PERCENT)) {
    tone = "critical";
  } else if (
    !hasReset &&
    (window.status === "warning" || (used !== undefined && used >= RATE_LIMIT_WARNING_PERCENT))
  ) {
    tone = "warning";
  }

  const view: RateLimitWindowView = { window, used, hasReset, limited, tone };
  if (resetMs !== undefined && !hasReset) {
    view.resetsInMinutes = Math.max(0, Math.round((resetMs - now) / 60_000));
  }

  const windowMs = window.window_minutes * 60_000;
  if (hasReset || resetMs === undefined || windowMs <= 0) return view;
  const startMs = resetMs - windowMs;
  const elapsed = Math.min(1, Math.max(0, (observedMs - startMs) / windowMs));
  view.elapsedPercent = elapsed * 100;
  if (used === undefined || limited || elapsed < MIN_ELAPSED_FOR_PACE) return view;

  const ahead = used - view.elapsedPercent;
  if (ahead < RATE_LIMIT_PACE_TOLERANCE) return view;
  view.aheadOfPace = Math.round(ahead);
  // Project the observed burn rate forward from the moment it was observed.
  // Running ahead of the elapsed time always runs out before the reset.
  const usedPerMs = used / (observedMs - startMs);
  const runsOutAt = observedMs + (100 - used) / usedPerMs;
  view.runsOutInMinutes = Math.max(0, Math.round((runsOutAt - now) / 60_000));
  return view;
}

/**
 * The window closest to its limit, which is what a one-line summary shows.
 * A limited window always wins; windows without a reported share rank last.
 */
export function tightestRateLimitWindow(
  views: readonly RateLimitWindowView[],
): RateLimitWindowView | undefined {
  let best: RateLimitWindowView | undefined;
  const rank = (v: RateLimitWindowView) => (v.limited ? 1000 : (v.used ?? -1));
  for (const view of views) {
    if (!best || rank(view) > rank(best)) best = view;
  }
  return best;
}

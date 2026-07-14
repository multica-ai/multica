// FIR-3118: shared helpers for account usage rendering (Settings → Accounts
// list + account detail page). The provider reports how much of a rolling
// window is CONSUMED (usage_5h_pct / usage_7d_pct); the UI shows what is
// LEFT, usagepal-style.

import type { CerebroAccount } from "@multica/core/api";

/** Clamp a consumed-percentage to [0, 100]; null/NaN → null. */
export function clampPct(v: number | null | undefined): number | null {
  if (v === null || v === undefined || Number.isNaN(v)) return null;
  if (v < 0) return 0;
  if (v > 100) return 100;
  return v;
}

/**
 * Consumed % of the 5-hour window. A structured weekly window makes an
 * absent 5-hour window authoritative; only fully legacy records fall back
 * to the log-scraped usage_window_pct.
 */
export function usedPct5h(account: CerebroAccount): number | null {
  const exact5h = clampPct(account.usage_5h_pct);
  if (exact5h !== null) return exact5h;
  if (clampPct(account.usage_7d_pct) !== null) return null;
  return clampPct(account.usage_window_pct);
}

/** Consumed % of the 7-day (weekly) window, when the provider reports it. */
export function usedPct7d(account: CerebroAccount): number | null {
  return clampPct(account.usage_7d_pct);
}

export function remainingPct(usedPct: number | null): number | null {
  if (usedPct === null) return null;
  return 100 - usedPct;
}

export function formatTokens(value: number | null | undefined): string {
  const v = value ?? 0;
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `${(v / 1_000).toFixed(1)}k`;
  return new Intl.NumberFormat("en-US").format(v);
}

/**
 * Human-readable countdown to a window reset, e.g. "resets in 2h 15m".
 * Returns null for absent/unparseable/past timestamps.
 */
export function formatResetsIn(
  iso: string | null | undefined,
  now: number = Date.now(),
): string | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= now) return null;
  const totalMinutes = Math.round((t - now) / 60_000);
  const days = Math.floor(totalMinutes / (24 * 60));
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `resets in ${days}d ${hours}h`;
  if (hours > 0) return `resets in ${hours}h ${minutes}m`;
  return `resets in ${minutes}m`;
}

/** Meter color by how much of the window is LEFT. */
export function remainingBarColorClass(leftPct: number): string {
  if (leftPct <= 10) return "bg-red-500";
  if (leftPct <= 25) return "bg-amber-500";
  return "bg-emerald-500";
}

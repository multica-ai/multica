"use client";

import type { ProviderUsageSnapshot } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";

const PLAN_LIMIT_PROVIDERS = new Set([
  "claude",
  "cursor",
  "codex",
  "copilot",
  "antigravity",
  "grok",
  "kimi",
  "kiro",
  "opencode",
]);

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;

export function runtimeHasPlanUsage(provider: string | undefined): boolean {
  return PLAN_LIMIT_PROVIDERS.has(normalizeProvider(provider));
}

/** Snapshots for this runtime's protocol family. Other vendors on the same
 * machine, and families with no plan window, stay out of the detail view. */
export function planUsageSnapshotsForProvider(
  provider: string | undefined,
  providers: readonly ProviderUsageSnapshot[] | undefined,
): ProviderUsageSnapshot[] {
  const key = normalizeProvider(provider);
  if (!runtimeHasPlanUsage(key)) return [];
  return (providers ?? []).filter(
    (item) => normalizeProvider(item.provider) === key,
  );
}

interface RuntimePlanUsageCellProps {
  provider: string | undefined;
  providers: readonly ProviderUsageSnapshot[] | undefined;
  loading?: boolean;
  now?: number;
}

export function RuntimePlanUsageCell({
  provider,
  providers,
  loading = false,
  now,
}: RuntimePlanUsageCellProps) {
  const { t, i18n } = useT("runtimes");
  const locale = i18n.resolvedLanguage ?? i18n.language;
  if (loading && runtimeHasPlanUsage(provider)) {
    return <Skeleton className="h-4 w-10" />;
  }
  const headline = planUsageHeadline(provider, providers, locale, now ?? Date.now());
  if (!headline) {
    return <span className="text-caption text-faint-foreground">—</span>;
  }
  const used = t(($) => $.usage.provider_limits.used, { pct: headline.percent });
  const label = headline.detail ? `${used}, ${headline.detail}` : used;
  return (
    <div className="flex min-w-0 flex-col leading-tight" title={label}>
      <span className="text-body font-medium tabular-nums">{headline.percent}%</span>
      {headline.detail ? (
        <span className="truncate text-caption text-muted-foreground">{headline.detail}</span>
      ) : null}
      <span className="sr-only">{label}</span>
    </div>
  );
}

function planUsageHeadline(
  provider: string | undefined,
  providers: readonly ProviderUsageSnapshot[] | undefined,
  locale: string,
  now: number,
): { percent: number; detail: string } | null {
  const key = normalizeProvider(provider);
  const snapshot = planUsageSnapshotsForProvider(key, providers)[0];
  const windows = snapshot?.windows ?? [];
  const preferred = headlineWindowId(key);
  const headline =
    windows.find((window) => window.id === preferred && usablePercent(window.percent_used)) ??
    windows.find((window) => usablePercent(window.percent_used));
  if (!headline || !usablePercent(headline.percent_used)) return null;
  const reset = shortResetLabel(headline.resets_at, locale, now);
  const plan = snapshot?.plan_name?.trim() ?? "";
  return {
    percent: Math.round(headline.percent_used),
    detail: reset || plan,
  };
}

function headlineWindowId(provider: string): string {
  switch (provider) {
    case "claude":
      return "session";
    case "cursor":
      return "auto";
    case "codex":
      return "primary";
    case "copilot":
      return "premium_interactions";
    case "kimi":
    case "opencode":
      return "rolling";
    case "grok":
    case "kiro":
      return "credits";
    case "antigravity":
      return "gemini_hourly";
    default:
      return "";
  }
}

function usablePercent(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1000;
}

function shortResetLabel(value: string | undefined, locale: string, now: number): string {
  if (!value) return "";
  const time = new Date(value).getTime();
  if (!Number.isFinite(time)) return "";
  const delta = time - now;
  if (delta <= 0 || delta > 14 * DAY_MS) return "";
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "always", style: "narrow" });
  if (delta < HOUR_MS) {
    return rtf.format(Math.max(1, Math.round(delta / MINUTE_MS)), "minute");
  }
  if (delta < 2 * DAY_MS) {
    return rtf.format(Math.round(delta / HOUR_MS), "hour");
  }
  return rtf.format(Math.round(delta / DAY_MS), "day");
}

function normalizeProvider(provider: string | undefined): string {
  return provider?.trim().toLowerCase() ?? "";
}

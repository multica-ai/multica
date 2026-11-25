"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Gauge } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import type {
  AgentRuntime,
  RuntimeRateLimits,
  RuntimeRateLimitWindow,
} from "@multica/core/types";
import { runtimeRateLimitsOptions } from "@multica/core/runtimes/queries";
import {
  describeRateLimitWindow,
  runtimeReportsRateLimits,
  tightestRateLimitWindow,
  type RateLimitTone,
  type RateLimitWindowView,
} from "@multica/core/runtimes/rate-limits";
import { useT, useTimeAgo } from "../../i18n";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { ObjectSectionCard, ObjectSectionEmpty } from "../../layout/object-page";
import { CollectionEmptyValue } from "../../layout/collection-list";

// Runtime rate limits (MUL-9427): the plan windows of the account the
// runtime's agent CLI runs under, as the CLI last reported them while a task
// ran. The detail page shows every window; the runtime list shows the window
// closest to its limit.

const FILL_TONE: Record<RateLimitTone, string> = {
  normal: "bg-brand",
  warning: "bg-warning",
  critical: "bg-destructive",
};

const TEXT_TONE: Record<RateLimitTone, string> = {
  normal: "",
  warning: "text-warning",
  critical: "text-destructive",
};

// Re-evaluates countdowns and pace between refetches.
function useMinuteTick(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);
  return now;
}

function useRateLimitText() {
  const { t, i18n } = useT("runtimes");
  const tz = useViewingTimezone();
  const locale = i18n.resolvedLanguage ?? i18n.language;
  // Weekday and time are formatted apart: a combined zh format renders
  // "周四14:54" with no separator.
  const resetFormat = useMemo(() => {
    const weekday = new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: tz });
    const time = new Intl.DateTimeFormat(locale, {
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      timeZone: tz,
    });
    return (date: Date) => `${weekday.format(date)} ${time.format(date)}`;
  }, [locale, tz]);
  const creditFormat = useMemo(
    () => new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }),
    [locale],
  );

  // Codex reports the balance as a fixed-point string ("62500.0000000000").
  const creditBalance = (balance: string) => {
    const n = Number(balance);
    return balance.trim() !== "" && Number.isFinite(n) ? creditFormat.format(n) : balance;
  };

  const duration = (minutes: number) => {
    if (minutes < 60) {
      return t(($) => $.rate_limits.duration_minutes, { minutes: Math.max(1, minutes) });
    }
    if (minutes < 24 * 60) {
      return t(($) => $.rate_limits.duration_hours, {
        hours: Math.floor(minutes / 60),
        minutes: minutes % 60,
      });
    }
    return t(($) => $.rate_limits.duration_days, {
      days: Math.floor(minutes / (24 * 60)),
      hours: Math.floor((minutes % (24 * 60)) / 60),
    });
  };

  const windowLabel = (window: RuntimeRateLimitWindow) => {
    switch (window.kind) {
      case "session":
        return window.window_minutes > 0 && window.window_minutes % 60 === 0
          ? t(($) => $.rate_limits.window_session_hours, { hours: window.window_minutes / 60 })
          : t(($) => $.rate_limits.window_session);
      case "weekly":
        return t(($) => $.rate_limits.window_weekly);
      case "weekly_opus":
        return t(($) => $.rate_limits.window_weekly_opus);
      case "weekly_sonnet":
        return t(($) => $.rate_limits.window_weekly_sonnet);
      default:
        return t(($) => $.rate_limits.window_generic);
    }
  };

  const shortLabel = (window: RuntimeRateLimitWindow) => {
    switch (window.kind) {
      case "session":
        return t(($) => $.rate_limits.short_session);
      case "weekly":
        return t(($) => $.rate_limits.short_weekly);
      case "weekly_opus":
        return t(($) => $.rate_limits.short_weekly_opus);
      case "weekly_sonnet":
        return t(($) => $.rate_limits.short_weekly_sonnet);
      default:
        return t(($) => $.rate_limits.short_generic);
    }
  };

  const reset = (view: RateLimitWindowView) => {
    if (view.hasReset) return t(($) => $.rate_limits.reset_since);
    if (view.resetsInMinutes === undefined || !view.window.resets_at) return null;
    if (view.resetsInMinutes < 24 * 60) {
      return t(($) => $.rate_limits.resets_in, { duration: duration(view.resetsInMinutes) });
    }
    return t(($) => $.rate_limits.resets_at, {
      time: resetFormat(new Date(view.window.resets_at)),
    });
  };

  const usage = (view: RateLimitWindowView) => {
    if (view.limited) return t(($) => $.rate_limits.limited);
    if (view.used === undefined) return null;
    return t(($) => $.rate_limits.used, { pct: Math.round(view.used) });
  };

  const verdict = (view: RateLimitWindowView): { text: string; tone: RateLimitTone } | null => {
    if (view.hasReset) return null;
    if (view.limited) return { text: t(($) => $.rate_limits.verdict_limited), tone: "critical" };
    if (view.aheadOfPace !== undefined && view.runsOutInMinutes !== undefined) {
      return {
        text: t(($) => $.rate_limits.verdict_ahead, {
          pct: view.aheadOfPace,
          duration: duration(view.runsOutInMinutes),
        }),
        tone: view.tone === "critical" ? "critical" : "warning",
      };
    }
    if (view.used !== undefined && view.elapsedPercent !== undefined) {
      return { text: t(($) => $.rate_limits.verdict_on_pace), tone: "normal" };
    }
    return null;
  };

  return { t, windowLabel, shortLabel, reset, usage, verdict, creditBalance };
}

function useWindowViews(rateLimits: RuntimeRateLimits | null | undefined, now: number) {
  return useMemo(
    () =>
      rateLimits
        ? rateLimits.windows.map((w) => describeRateLimitWindow(w, rateLimits.observed_at, now))
        : [],
    [rateLimits, now],
  );
}

function planLabel(plan: string): string {
  const words = plan.replace(/[_-]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : "";
}

function Meter({
  view,
  label,
  className,
}: {
  view: RateLimitWindowView;
  label: string;
  className?: string;
}) {
  const used = view.limited ? 100 : (view.used ?? 0);
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(Math.min(used, 100))}
      className={cn("relative h-2 rounded-full bg-foreground/10", className)}
    >
      <div
        className={cn(
          "absolute inset-y-0 left-0 rounded-full transition-[width,background-color] duration-200 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
          FILL_TONE[view.tone],
        )}
        style={{ width: `${Math.min(used, 100)}%` }}
      />
      {view.elapsedPercent !== undefined && !view.limited && (
        // Pace marker: the share of the window that had elapsed when the
        // usage was reported. Fill past it means usage runs ahead of time.
        <div
          aria-hidden="true"
          data-slot="rate-limit-pace"
          className="absolute -inset-y-1 w-0.5 -translate-x-1/2 rounded-full bg-foreground/50 transition-[left] duration-200 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none"
          style={{ left: `${view.elapsedPercent}%` }}
        />
      )}
    </div>
  );
}

function WindowRow({ view }: { view: RateLimitWindowView }) {
  const text = useRateLimitText();
  const label = text.windowLabel(view.window);
  const verdict = text.verdict(view);
  const usage = text.usage(view);
  const reset = text.reset(view);
  return (
    <div className="grid grid-cols-[minmax(0,8.5rem)_minmax(0,1fr)_minmax(0,8.5rem)] items-center gap-x-5 px-4 py-3">
      <span className="truncate text-label font-medium">{label}</span>
      <div className="min-w-0 space-y-1.5">
        <Meter view={view} label={label} />
        {verdict && (
          <p className={cn("truncate text-caption text-muted-foreground", TEXT_TONE[verdict.tone])}>
            {verdict.text}
          </p>
        )}
      </div>
      <div className="min-w-0 text-right">
        <p className={cn("truncate text-body font-medium tabular-nums", TEXT_TONE[view.tone])}>
          {usage ?? "—"}
        </p>
        {reset && (
          <p className="truncate text-caption text-muted-foreground tabular-nums">{reset}</p>
        )}
      </div>
    </div>
  );
}

export function RateLimitSection({ runtime }: { runtime: AgentRuntime }) {
  const text = useRateLimitText();
  const timeAgo = useTimeAgo();
  const now = useMinuteTick();
  const { data, isLoading } = useQuery(runtimeRateLimitsOptions(runtime.id));
  const rateLimits = data?.rate_limits ?? null;
  const views = useWindowViews(rateLimits, now);

  if (isLoading) return <Skeleton className="h-32 w-full rounded-lg" />;
  if (!rateLimits || (views.length === 0 && !rateLimits.credits)) {
    return (
      <ObjectSectionEmpty
        icon={Gauge}
        title={text.t(($) => $.rate_limits.empty_title)}
        description={text.t(($) => $.rate_limits.empty_hint)}
      />
    );
  }

  const plan = planLabel(rateLimits.plan_type);
  const credits = rateLimits.credits;
  return (
    <ObjectSectionCard>
      <div className="flex min-w-0 items-center gap-2 px-4 py-2.5 text-caption text-muted-foreground">
        {plan && (
          <span className="shrink-0 rounded-sm bg-muted px-1.5 py-0.5 font-medium text-foreground">
            {plan}
          </span>
        )}
        <span className="min-w-0 truncate">{text.t(($) => $.rate_limits.caption)}</span>
        <span className="ml-auto shrink-0 tabular-nums">
          {text.t(($) => $.rate_limits.updated, { ago: timeAgo(rateLimits.observed_at) })}
        </span>
      </div>
      {views.map((view, index) => (
        <WindowRow key={`${view.window.kind}-${index}`} view={view} />
      ))}
      {credits && (
        <div className="flex items-center justify-between gap-4 px-4 py-3">
          <span className="text-label font-medium">{text.t(($) => $.rate_limits.credits)}</span>
          <span className="text-body font-medium tabular-nums">
            {credits.unlimited
              ? text.t(($) => $.rate_limits.credits_unlimited)
              : credits.has_credits && credits.balance
                ? text.t(($) => $.rate_limits.credits_balance, {
                    balance: text.creditBalance(credits.balance),
                  })
                : text.t(($) => $.rate_limits.credits_none)}
          </span>
        </div>
      )}
    </ObjectSectionCard>
  );
}

/**
 * Runtime-list cell: two stacked bars (the first two windows, usually the
 * session and the week) and the share of the window closest to its limit.
 * The list is already wider than the page at its default width, so the cell
 * stays compact and names the windows only in its tooltip.
 */
export function RateLimitCell({
  runtime,
  enabled,
}: {
  runtime: AgentRuntime;
  enabled: boolean;
}) {
  const supported = runtimeReportsRateLimits(runtime.provider);
  const text = useRateLimitText();
  const now = useMinuteTick();
  const { data } = useQuery({
    ...runtimeRateLimitsOptions(runtime.id),
    enabled: enabled && supported,
  });
  const rateLimits = data?.rate_limits ?? null;
  const views = useWindowViews(rateLimits, now);
  const tightest = tightestRateLimitWindow(views);
  if (!tightest) return <CollectionEmptyValue />;

  // A rejected window may not report a share; it is at its limit either way.
  const share = tightest.limited ? Math.max(tightest.used ?? 100, 100) : tightest.used;
  const summary = share !== undefined ? `${Math.round(share)}%` : text.shortLabel(tightest.window);
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="flex w-10 shrink-0 flex-col gap-0.5" aria-hidden="true">
              {views.slice(0, 2).map((view, index) => (
                <span
                  key={`${view.window.kind}-${index}`}
                  className={cn(
                    "relative overflow-hidden rounded-full bg-foreground/10",
                    index === 0 ? "h-1" : "h-0.5",
                  )}
                >
                  <span
                    className={cn("absolute inset-y-0 left-0 rounded-full", FILL_TONE[view.tone])}
                    style={{ width: `${Math.min(view.limited ? 100 : (view.used ?? 0), 100)}%` }}
                  />
                </span>
              ))}
            </span>
            <span
              className={cn(
                "truncate text-caption tabular-nums",
                TEXT_TONE[tightest.tone] || "text-muted-foreground",
              )}
            >
              {summary}
            </span>
          </span>
        }
      />
      <TooltipContent className="flex-col items-start gap-0.5">
        {views.map((view, index) => {
          const usage = text.usage(view);
          const reset = text.reset(view);
          return (
            <p key={`${view.window.kind}-${index}`} className="tabular-nums">
              {[text.windowLabel(view.window), usage, reset].filter(Boolean).join(" · ")}
            </p>
          );
        })}
      </TooltipContent>
    </Tooltip>
  );
}

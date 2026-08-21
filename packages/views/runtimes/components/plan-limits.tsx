"use client";

import { Gauge } from "lucide-react";
import type {
  AgentRuntime,
  PlanLimitWindow,
  PlanLimitsSnapshot,
} from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

const SNAPSHOT_MAX_AGE_MS = 24 * 60 * 60 * 1000;

export interface DisplayPlanLimits {
  snapshot: PlanLimitsSnapshot;
  windows: PlanLimitWindow[];
}

/**
 * Drops windows after their reset boundary and expires observations after a
 * day. This prevents the UI from presenting an old exhausted state or usage
 * percentage as current when a daemon has stopped reporting.
 */
export function displayPlanLimits(
  snapshot: PlanLimitsSnapshot | null | undefined,
  nowMs = Date.now(),
): DisplayPlanLimits | null {
  if (!snapshot || snapshot.observed_at <= 0) return null;

  const nowSeconds = Math.floor(nowMs / 1000);
  const reportedWindows = snapshot.windows ?? [];
  const windows = reportedWindows.filter(
    (window) => window.resets_at == null || window.resets_at > nowSeconds,
  );
  if (reportedWindows.length > 0 && windows.length === 0) return null;

  const observedAge = nowMs - snapshot.observed_at * 1000;
  if (observedAge > SNAPSHOT_MAX_AGE_MS) return null;
  if (snapshot.status === "available" && windows.length === 0) return null;

  return { snapshot, windows };
}

export function planLimitWindowShortLabel(window: PlanLimitWindow): string {
  if (window.window_minutes === 300) return "5h";
  if (window.window_minutes === 10_080) return "7d";
  return window.name;
}

function percentageTone(value: number): string {
  if (value >= 100) return "text-destructive";
  if (value >= 80) return "text-warning";
  return "text-foreground";
}

function percentageBarTone(value: number): string {
  if (value >= 100) return "bg-destructive";
  if (value >= 80) return "bg-warning";
  return "bg-primary";
}

export function PlanLimitsCell({
  runtime,
  now = Date.now(),
}: {
  runtime: AgentRuntime;
  now?: number;
}) {
  const { t } = useT("runtimes");
  const display = displayPlanLimits(runtime.plan_limits, now);
  if (!display) {
    return <span className="text-caption text-faint-foreground">—</span>;
  }

  const percentages = display.windows.filter(
    (window): window is PlanLimitWindow & { used_percent: number } =>
      window.used_percent != null,
  );
  if (percentages.length === 0) {
    return (
      <span className="truncate text-caption font-medium text-destructive">
        {t(($) => $.plan_limits.limit_reached)}
      </span>
    );
  }

  return (
    <div
      className="flex min-w-0 flex-col leading-tight"
      aria-label={t(($) => $.plan_limits.title)}
    >
      {percentages.slice(0, 2).map((window) => (
        <span
          key={window.name}
          className={`truncate text-caption tabular-nums ${percentageTone(window.used_percent)}`}
        >
          <span className="text-muted-foreground">
            {planLimitWindowShortLabel(window)}
          </span>{" "}
          {Math.round(window.used_percent)}%
        </span>
      ))}
    </div>
  );
}

function windowLabel(
  window: PlanLimitWindow,
  t: ReturnType<typeof useT<"runtimes">>["t"],
): string {
  if (window.window_minutes === 300) {
    return t(($) => $.plan_limits.window_5h);
  }
  if (window.window_minutes === 10_080) {
    return t(($) => $.plan_limits.window_7d);
  }
  if (window.name === "primary") {
    return t(($) => $.plan_limits.window_primary);
  }
  if (window.name === "secondary") {
    return t(($) => $.plan_limits.window_secondary);
  }
  return window.name;
}

export function PlanLimitsCard({
  runtime,
  now = Date.now(),
}: {
  runtime: AgentRuntime;
  now?: number;
}) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const display = displayPlanLimits(runtime.plan_limits, now);
  const observed = display
    ? timeAgo(new Date(display.snapshot.observed_at * 1000).toISOString())
    : null;

  return (
    <section className="rounded-lg border bg-card">
      <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <Gauge className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-body font-semibold">
            {t(($) => $.plan_limits.title)}
          </h3>
        </div>
        {observed && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.plan_limits.observed, { when: observed })}
          </span>
        )}
      </div>

      {!display ? (
        <div className="px-4 py-5">
          <p className="text-body font-medium">
            {t(($) => $.plan_limits.unavailable)}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">
            {runtime.provider === "claude"
              ? t(($) => $.plan_limits.unavailable_hint_claude)
              : t(($) => $.plan_limits.unavailable_hint)}
          </p>
        </div>
      ) : display.windows.length === 0 ? (
        <div className="px-4 py-5">
          <p className="text-body font-medium text-destructive">
            {t(($) => $.plan_limits.limit_reached)}
          </p>
        </div>
      ) : (
        <div className="divide-y">
          {display.windows.map((window) => {
            const used = window.used_percent;
            const reset = window.resets_at
              ? timeAgo(new Date(window.resets_at * 1000).toISOString())
              : null;
            return (
              <div key={window.name} className="px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-caption font-medium">
                    {windowLabel(window, t)}
                  </span>
                  {used != null ? (
                    <span
                      className={`text-caption font-semibold tabular-nums ${percentageTone(used)}`}
                    >
                      {t(($) => $.plan_limits.used, {
                        percent: Math.round(used),
                      })}
                    </span>
                  ) : (
                    <span className="text-caption font-medium text-destructive">
                      {t(($) => $.plan_limits.limit_reached)}
                    </span>
                  )}
                </div>
                {used != null && (
                  <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted">
                    <div
                      className={`h-full rounded-full ${percentageBarTone(used)}`}
                      style={{ width: `${Math.min(100, used)}%` }}
                    />
                  </div>
                )}
                {reset && (
                  <p className="mt-1.5 text-caption text-muted-foreground">
                    {t(($) => $.plan_limits.resets, { when: reset })}
                  </p>
                )}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

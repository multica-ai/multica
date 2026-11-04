"use client";

import type { GlmQuotaStatus, GlmQuotaWindow } from "@multica/core/api";
import {
  formatCompactDuration,
  isCollectorObservationInterrupted,
  quotaTone,
  type QuotaTone,
} from "@multica/core/runtimes";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";
import { useQuotaTimeFormatter } from "./quota-time";
import { quotaInterruptedText } from "./runtime-quota-cell";
import zhipuLogo from "./zhipu-logo.svg";

// Next.js exposes static imports as objects while Vite exposes URL strings;
// normalize so the shared card works in web and desktop (same helper as
// provider-logo.tsx, kept local because that one is not exported).
function staticAssetSrc(asset: string | { src: string }): string {
  return typeof asset === "string" ? asset : asset.src;
}

// The fixed, account-level GLM (Zhipu) Coding Plan balance card for the
// runtimes page. The server queries its configured provider key independently
// of any runtime, so the balance is shown once above the machine list.

const CHIP_TONE_CLASS: Record<QuotaTone, string> = {
  ok: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  destructive: "bg-destructive/10 text-destructive",
};

// A window whose reset has passed counts as refilled, like runtime quota.
export function glmWindowResetPassed(w: GlmQuotaWindow, nowSec: number): boolean {
  return w.resets_at != null && w.resets_at <= nowSec;
}

// Remaining percent per window: the provider's percentage is *used*, and
// credit windows may omit it while carrying remaining/usage — compute from
// whichever pair exists. Null means "not derivable" (chip shows unknown).
export function glmWindowRemainingPercent(
  w: GlmQuotaWindow,
  nowSec: number,
): number | null {
  if (glmWindowResetPassed(w, nowSec)) return 100;
  if (w.used_percent != null) return Math.max(0, 100 - w.used_percent);
  if (w.remaining != null && w.usage != null && w.usage > 0) {
    return Math.max(0, Math.min(100, Math.round((w.remaining / w.usage) * 100)));
  }
  return null;
}

// Worst window first: the lowest remaining percent leads, so the card's
// first chip is the one a viewer would act on.
export function glmWorstFirst(
  windows: GlmQuotaWindow[],
  nowSec: number,
): GlmQuotaWindow[] {
  return [...windows].sort(
    (a, b) =>
      (glmWindowRemainingPercent(a, nowSec) ?? 101) -
      (glmWindowRemainingPercent(b, nowSec) ?? 101),
  );
}

// The window's period class derived from window_minutes (the provider's
// unit/number converted server-side). Null when the provider sent no
// recognizable period — such a window must fall back to the generic type
// label, never to a specific one (two TOKENS_LIMIT windows can only be told
// apart by it, OL-1205).
export function glmWindowPeriod(
  w: GlmQuotaWindow,
): "five_hour" | "weekly" | null {
  if (w.window_minutes === 300) return "five_hour";
  if (w.window_minutes === 10080) return "weekly";
  return null;
}

export function GlmQuotaCard({
  data,
  now,
}: {
  data: GlmQuotaStatus | undefined;
  now: number;
}) {
  const { t } = useT("quota");
  const formatTime = useQuotaTimeFormatter();

  // Unconfigured (no server key) hides the card entirely — the surface is
  // opt-in per deployment.
  if (!data || !data.enabled || !data.quota) return null;

  const nowSec = Math.floor(now / 1000);
  const windows = glmWorstFirst(data.quota.windows ?? [], nowSec);
  if (windows.length === 0) return null;
  const observedAt = data.quota.observed_at;
  // The server polls every five minutes; an hour of silence means the
  // collector stopped, the same rule as the runtime collectors.
  const interrupted = isCollectorObservationInterrupted(observedAt, now);

  return (
    <div className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg border bg-card px-3 py-2">
      <span className="flex items-center gap-2">
        <img src={staticAssetSrc(zhipuLogo)} alt="Zhipu" className="h-4 w-4" />
        <span className="text-sm font-medium">{t(($) => $.glm_title)}</span>
        {data.quota.level ? (
          <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            {data.quota.level}
          </span>
        ) : null}
        {interrupted ? (
          <span className="text-xs tabular-nums text-muted-foreground">
            {quotaInterruptedText(observedAt, now, t, formatTime)}
          </span>
        ) : null}
      </span>
      <span className="flex flex-wrap items-center gap-1.5">
        {windows.map((w, i) => {
          const passed = glmWindowResetPassed(w, nowSec);
          const remaining = glmWindowRemainingPercent(w, nowSec);
          const tone = quotaTone(remaining);
          const period = glmWindowPeriod(w);
          const label =
            w.type === "TOKENS_LIMIT"
              ? period === "weekly"
                ? t(($) => $.glm_window_tokens_weekly)
                : period === "five_hour"
                  ? t(($) => $.glm_window_tokens)
                  : t(($) => $.glm_window_tokens_generic)
              : w.type === "TIME_LIMIT"
                ? t(($) => $.glm_window_time)
                : w.type === "CREDIT_LIMIT"
                  ? period === "weekly"
                    ? t(($) => $.glm_window_credit_weekly)
                    : period === "five_hour"
                      ? t(($) => $.glm_window_credit)
                      : t(($) => $.glm_window_credit_generic)
                  : t(($) => $.glm_window_fallback, { type: w.type });
          const resetTime = w.resets_at != null ? formatTime(w.resets_at, now) : null;
          const reset =
            resetTime == null
              ? null
              : passed
                ? t(($) => $.reset_passed, { time: resetTime })
                : t(($) => $.resets_at, { time: resetTime });
          return (
            <Tooltip key={`${w.type}-${i}`}>
              <TooltipTrigger
                render={
                  <span
                    className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs ${interrupted ? "bg-muted text-muted-foreground" : CHIP_TONE_CLASS[tone]}`}
                  >
                    <span className="text-muted-foreground">{label}</span>
                    {remaining != null ? (
                      <span>{t(($) => $.remaining, { percent: remaining })}</span>
                    ) : (
                      <span>{t(($) => $.unknown)}</span>
                    )}
                  </span>
                }
              />
              <TooltipContent>
                <div className="space-y-0.5 text-xs">
                  {/* A refilled window's old counters no longer apply. */}
                  {!passed && w.remaining != null && w.usage != null ? (
                    <div>
                      {w.current_value ?? 0} / {w.usage}
                    </div>
                  ) : null}
                  {reset ? <div>{reset}</div> : null}
                  <div className="text-muted-foreground">
                    {interrupted
                      ? quotaInterruptedText(observedAt, now, t, formatTime)
                      : t(($) => $.observed_ago, {
                          time: formatCompactDuration(now - observedAt * 1000),
                        })}
                  </div>
                </div>
              </TooltipContent>
            </Tooltip>
          );
        })}
      </span>
    </div>
  );
}

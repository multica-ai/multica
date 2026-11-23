"use client";

import { parseWithFallback } from "@multica/core/api/schema";
import { futureResetAt, llmLimitStatusSchema, unavailableLlmLimitStatus, type LlmLimitStatus } from "@multica/core/api/schemas";


import { useEffect, useState } from "react";
import { useT } from "../../i18n";
import { RefreshCw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";

async function fetchLlmLimitStatus(): Promise<LlmLimitStatus> {
  const response = await fetch("/api/dashboard/llm-limit-status", {
    cache: "no-store",
    credentials: "include",
  });
  if (!response.ok) {
    throw new Error("Failed to load LLM limit status");
  }
  return parseWithFallback(await response.json(), llmLimitStatusSchema, unavailableLlmLimitStatus, { endpoint: "/api/dashboard/llm-limit-status" });
}

function remainingFromUsage(value: number | null | undefined): number | null {
  if (!Number.isFinite(value)) return null;
  return Math.max(0, Math.min(100, 100 - Math.round(value ?? 0)));
}

function limitingRemaining(...usageValues: Array<number | null | undefined>): number | null {
  const remainingValues = usageValues
    .map(remainingFromUsage)
    .filter((value): value is number => value !== null);
  return remainingValues.length > 0 ? Math.min(...remainingValues) : null;
}

function compactRemainingLabel(value: number | null): string {
  return value === null ? "확인 불가" : `${value}%`;
}

// 2026-11-23 대표님: "리셋 D-day 언제인지가 나와야지 몇 일 몇 시간 몇 분" — 사용률이 높은 창을 고르면
// 5시간 창이 주간 창보다 높을 때 C가 "2시간 뒤"로 바뀌어 주간 리셋과 안 맞아 보였다.
// 주간(7일) 리셋을 항상 우선하고, 없을 때만 다음 창으로 내려간다.
function weeklyFirstReset(
  now: number,
  ...resets: ReadonlyArray<string | null | undefined>
): string | null {
  for (const reset of resets) {
    const target = futureResetAt(reset, now);
    if (target !== null) return target;
  }
  return null;
}

function ResetCountdown({ data }: { data: LlmLimitStatus }) {
  const { t } = useT("chat");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const providers = [
    ["C", weeklyFirstReset(now, data.seven_day_resets_at, data.sonnet_resets_at, data.five_hour_resets_at)],
    ["G", weeklyFirstReset(now, data.gpt_seven_resets_at, data.gpt_five_resets_at)],
  ] as const;
  return (
    <div
      data-testid="chat-reset-countdown"
      className="min-w-0 overflow-x-auto whitespace-nowrap pb-0.5 text-[10px] leading-4 tabular-nums"
      aria-label={t($ => $.limit_reset.countdown_label)}
    >
      {providers.map(([provider, target], index) => {
        const totalMinutes = target ? Math.max(0, Math.floor((Date.parse(target) - now) / 60_000)) : 0;
        const duration = {
          days: Math.floor(totalMinutes / 1440),
          hours: Math.floor(totalMinutes / 60) % 24,
          minutes: totalMinutes % 60,
        };
        return (
          <span key={provider}>
            {index > 0 && " · "}
            {provider} {t($ => $.limit_reset.label)}{" "}
            {target
              ? duration.days > 0
                ? t($ => $.limit_reset.duration_days, duration)
                : t($ => $.limit_reset.duration, duration)
              : t($ => $.limit_reset.unknown)}
          </span>
        );
      })}
    </div>
  );
}

export function retainKnownQuota(previous: LlmLimitStatus | undefined, current: LlmLimitStatus): LlmLimitStatus {
  const result = { ...current };
  if (current.five_hour_pct === null || current.seven_day_pct === null) {
    result.five_hour_pct ??= current.claude_last_five_hour_pct ?? previous?.five_hour_pct ?? null;
    result.seven_day_pct ??= current.claude_last_seven_day_pct ?? previous?.seven_day_pct ?? null;
    result.sonnet_pct ??= previous?.sonnet_pct ?? null;
    result.claude_last_observed_at ??= previous?.claude_last_observed_at;
    result.five_hour_resets_at = futureResetAt(current.five_hour_resets_at) ?? futureResetAt(previous?.five_hour_resets_at);
    result.seven_day_resets_at = futureResetAt(current.seven_day_resets_at) ?? futureResetAt(previous?.seven_day_resets_at);
    result.sonnet_resets_at = futureResetAt(current.sonnet_resets_at) ?? futureResetAt(previous?.sonnet_resets_at);
    if (result.five_hour_pct !== null || result.seven_day_pct !== null) result.claude_status = current.claude_status === "available" ? "stale" : current.claude_status ?? "unavailable";
  }
  if (current.gpt_five_hour_pct === null || current.gpt_seven_day_pct === null) {
    result.gpt_five_hour_pct ??= previous?.gpt_five_hour_pct ?? null;
    result.gpt_seven_day_pct ??= previous?.gpt_seven_day_pct ?? null;
    result.gpt_last_observed_at ??= previous?.gpt_last_observed_at;
    if (result.gpt_five_hour_pct !== null || result.gpt_seven_day_pct !== null) result.gpt_status = current.gpt_status === "available" ? "stale" : current.gpt_status ?? "unavailable";
  }
  return result;
}

export function LlmRemainingBadge({ className }: { className?: string }) {
  const { t } = useT("chat");
  const { data: fetchedData, isError, isFetching, refetch } = useQuery({
    queryKey: ["chat-llm-limit-status"],
    queryFn: fetchLlmLimitStatus,
    structuralSharing: (previous, current) => retainKnownQuota(previous as LlmLimitStatus | undefined, current as LlmLimitStatus),
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  const data = isError ? retainKnownQuota(fetchedData, unavailableLlmLimitStatus) : fetchedData;
  if (!data) return null;

  const claudeFiveHourRemaining = remainingFromUsage(data.five_hour_pct);
  const claudeSevenDayRemaining = limitingRemaining(data.seven_day_pct, data.sonnet_pct);
  const gptFiveHourRemaining = remainingFromUsage(data.gpt_five_hour_pct);
  const gptSevenDayRemaining = remainingFromUsage(data.gpt_seven_day_pct);

  const ariaLabel = [
    `채팅 LLM 잔량: Claude 5시간 ${compactRemainingLabel(claudeFiveHourRemaining)}`,
    `Claude 1주 ${compactRemainingLabel(claudeSevenDayRemaining)}`,
    `GPT 5시간 ${compactRemainingLabel(gptFiveHourRemaining)}`,
    `GPT 1주 ${compactRemainingLabel(gptSevenDayRemaining)}`,
  ].join(", ");
  const claudeLastKnown = data.claude_status !== undefined && data.claude_status !== "available" && (claudeFiveHourRemaining !== null || claudeSevenDayRemaining !== null);
  const gptLastKnown = data.gpt_status !== undefined && data.gpt_status !== "available" && (gptFiveHourRemaining !== null || gptSevenDayRemaining !== null);
  const lastKnownTitle = [
    claudeLastKnown ? t($ => $.limit_reset.last_known, {provider: "C*", time: data.claude_last_observed_at ?? "--"}) : "",
    gptLastKnown ? t($ => $.limit_reset.last_known, {provider: "G*", time: data.gpt_last_observed_at ?? "--"}) : "",
  ].filter(Boolean).join(" · ");
  const compactLabel = [
    `C${claudeLastKnown ? "*" : ""}: 5h ${compactRemainingLabel(claudeFiveHourRemaining)}/7d ${compactRemainingLabel(claudeSevenDayRemaining)}`,
    `G${gptLastKnown ? "*" : ""}: 5h ${compactRemainingLabel(gptFiveHourRemaining)}/7d ${compactRemainingLabel(gptSevenDayRemaining)}`,
  ].join(" · ");

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      data-testid="chat-token-gauge"
      className={cn(
        "flex min-w-0 flex-col gap-0.5 rounded-md border px-2 py-1 text-[10px] leading-none text-muted-foreground",
        className,
      )}
      title={lastKnownTitle || (data.claude_last_observed_at ? `Claude 마지막 관측 ${new Date(data.claude_last_observed_at).toLocaleString()}` : undefined)}
      aria-label={[ariaLabel, lastKnownTitle].filter(Boolean).join(" · ")}
    >
      <div className="flex w-full min-w-0 items-center gap-1.5">
      <span
        data-acceptance="chat-token-gauge"
        data-testid="chat-llm-gauge-compact"
        className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap font-medium tabular-nums text-foreground"
      >
        {compactLabel}
      </span>
      <span
        data-acceptance="chat-claude-token-remaining-badge"
        data-testid="chat-llm-gauge-claude"
        aria-label={`Claude 5시간 잔량 ${compactRemainingLabel(claudeFiveHourRemaining)}; Claude 1주 잔량 ${compactRemainingLabel(claudeSevenDayRemaining)}`}
        className="sr-only"
      />
      <span
        data-acceptance="chat-gpt-token-remaining-badge"
        data-testid="chat-llm-gauge-gpt"
        aria-label={`GPT 5시간 잔량 ${compactRemainingLabel(gptFiveHourRemaining)}; GPT 1주 잔량 ${compactRemainingLabel(gptSevenDayRemaining)}`}
        className="sr-only"
      />
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        className="self-center"
        data-acceptance="chat-llm-gauge-manual-refresh"
        aria-label="채팅 LLM 잔량 새로고침"
        onClick={() => void refetch()}
      >
        <RefreshCw className={cn("h-3 w-3", isFetching && "animate-spin")} />
      </Button>
      </div>
      <ResetCountdown data={data} />
    </div>
  );
}

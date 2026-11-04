"use client";

import { parseWithFallback } from "@multica/core/api/schema";
import { llmLimitStatusSchema, unavailableLlmLimitStatus, type LlmLimitStatus } from "@multica/core/api/schemas";


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

function compactResetLabel(label: string | undefined): string {
  if (!label || label === "—" || label === "-") return "-";
  return label
    .replace(/^resets\s+/i, "")
    .replace(/에 재설정$/, "")
    .trim();
}

function limitingClaudeSevenDayResetLabel(data: LlmLimitStatus): string | undefined {
  const sevenDayRemaining = remainingFromUsage(data.seven_day_pct);
  const sonnetRemaining = remainingFromUsage(data.sonnet_pct);
  if (sevenDayRemaining === null) return data.sonnet_reset_label;
  if (sonnetRemaining === null) return data.seven_day_reset_label;
  return sonnetRemaining < sevenDayRemaining ? data.sonnet_reset_label : data.seven_day_reset_label;
}

function compactRemainingLabel(value: number | null): string {
  return value === null ? "확인 불가" : `${value}%`;
}

export function formatResetCountdown(resetAt: string | null | undefined, now: number): string {
  const target = resetAt ? Date.parse(resetAt) : Number.NaN;
  if (!Number.isFinite(target)) return "--:--:--";
  const seconds = Math.max(0, Math.ceil((target - now) / 1000));
  const days = Math.floor(seconds / 86400);
  const time = [Math.floor(seconds / 3600) % 24, Math.floor(seconds / 60) % 60, seconds % 60]
    .map((value) => String(value).padStart(2, "0")).join(":");
  return `${days ? `D-${days} ` : ""}${time}`;
}

function ResetCountdown({ data }: { data: LlmLimitStatus }) {
  const { t } = useT("chat");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const sonnetLimits = remainingFromUsage(data.sonnet_pct) !== null &&
    (remainingFromUsage(data.seven_day_pct) === null || data.sonnet_pct! > data.seven_day_pct!);
  const resets = [
    ["C", sonnetLimits ? data.sonnet_resets_at : data.seven_day_resets_at],
    ["G", data.gpt_seven_resets_at],
  ] as const;
  return <div data-testid="chat-reset-countdown" className="@container max-w-full pb-0.5 text-[10px] leading-4 tabular-nums" aria-label={t($ => $.limit_reset.countdown_label)}>
    <div className="flex flex-col @[330px]:flex-row @[330px]:gap-2">
    {resets.map(([label, resetAt], index) => <span key={label} className="whitespace-nowrap">
      {index > 0 && <span aria-hidden="true" className="hidden pr-2 @[330px]:inline">|</span>}
      <span title={resetAt ? t($ => $.limit_reset.at, { time: new Intl.DateTimeFormat("ko-KR", { timeZone: "Asia/Seoul", dateStyle: "full", timeStyle: "long" }).format(new Date(resetAt)) }) : t($ => $.limit_reset.unknown)}>
        {label} {t($ => $.limit_reset.label)} {formatResetCountdown(resetAt, now)}
      </span>
    </span>)}
    </div>
  </div>;
}

export function LlmRemainingBadge({ className }: { className?: string }) {
  const { data: fetchedData, isError, isFetching, refetch } = useQuery({
    queryKey: ["chat-llm-limit-status"],
    queryFn: fetchLlmLimitStatus,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  const data = isError ? unavailableLlmLimitStatus : fetchedData;
  if (!data) return null;

  const claudeFiveHourRemaining = remainingFromUsage(data.five_hour_pct);
  const claudeSevenDayRemaining = limitingRemaining(data.seven_day_pct, data.sonnet_pct);
  const gptFiveHourRemaining = remainingFromUsage(data.gpt_five_hour_pct);
  const gptSevenDayRemaining = remainingFromUsage(data.gpt_seven_day_pct);
  const claudeFiveHourReset = compactResetLabel(data.five_hour_reset_label);
  const claudeSevenDayReset = compactResetLabel(limitingClaudeSevenDayResetLabel(data));
  const gptFiveHourReset = compactResetLabel(data.gpt_five_reset_label);
  const gptSevenDayReset = compactResetLabel(data.gpt_seven_reset_label);

  const ariaLabel = [
    `채팅 LLM 잔량: Claude 5시간 ${compactRemainingLabel(claudeFiveHourRemaining)}, 리셋 ${claudeFiveHourReset}`,
    `Claude 1주 ${compactRemainingLabel(claudeSevenDayRemaining)}, 리셋 ${claudeSevenDayReset}`,
    `GPT 5시간 ${gptFiveHourRemaining === null ? "확인 불가" : `${gptFiveHourRemaining}%`}, 리셋 ${gptFiveHourReset}`,
    `GPT 1주 ${gptSevenDayRemaining === null ? "확인 불가" : `${gptSevenDayRemaining}%`}, 리셋 ${gptSevenDayReset}`,
  ].join(", ");
  const compactLabel = [
    `C: 5h ${compactRemainingLabel(claudeFiveHourRemaining)}/7d ${compactRemainingLabel(claudeSevenDayRemaining)}`,
    `G: 5h ${compactRemainingLabel(gptFiveHourRemaining)}/7d ${compactRemainingLabel(gptSevenDayRemaining)}`,
  ].join(" · ");

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      data-testid="chat-token-gauge"
      className={cn(
        "flex min-w-0 flex-col gap-0.5 rounded-md border px-2 py-1 text-[10px] leading-none text-muted-foreground",
        className,
      )}
      title={data.claude_last_observed_at ? `Claude 마지막 관측 ${new Date(data.claude_last_observed_at).toLocaleString()}` : undefined}
      aria-label={ariaLabel}
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
        aria-label={`Claude 5시간 잔량 ${compactRemainingLabel(claudeFiveHourRemaining)}, 리셋 ${claudeFiveHourReset}; Claude 1주 잔량 ${compactRemainingLabel(claudeSevenDayRemaining)}, 리셋 ${claudeSevenDayReset}`}
        className="sr-only"
      />
      <span
        data-acceptance="chat-gpt-token-remaining-badge"
        data-testid="chat-llm-gauge-gpt"
        aria-label={`GPT 5시간 잔량 ${gptFiveHourRemaining === null ? "확인 불가" : `${gptFiveHourRemaining}%`}, 리셋 ${gptFiveHourReset}; GPT 1주 잔량 ${gptSevenDayRemaining === null ? "확인 불가" : `${gptSevenDayRemaining}%`}, 리셋 ${gptSevenDayReset}`}
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

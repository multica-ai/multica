"use client";

import { RefreshCw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";

interface LlmLimitStatus {
  five_hour_pct: number;
  seven_day_pct: number;
  sonnet_pct: number;
  gpt_five_hour_pct: number | null;
  gpt_seven_day_pct: number | null;
  five_hour_reset_label?: string;
  seven_day_reset_label?: string;
  sonnet_reset_label?: string;
  gpt_five_reset_label?: string;
  gpt_seven_reset_label?: string;
}

async function fetchLlmLimitStatus(): Promise<LlmLimitStatus> {
  const response = await fetch("/api/dashboard/llm-limit-status", {
    cache: "no-store",
    credentials: "include",
  });
  if (!response.ok) {
    throw new Error("Failed to load LLM limit status");
  }
  return response.json() as Promise<LlmLimitStatus>;
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

export function LlmRemainingBadge({ className }: { className?: string }) {
  const { data, isFetching, refetch } = useQuery({
    queryKey: ["chat-llm-limit-status"],
    queryFn: fetchLlmLimitStatus,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

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
    `채팅 LLM 잔량: Claude 5시간 ${claudeFiveHourRemaining}%, 리셋 ${claudeFiveHourReset}`,
    `Claude 1주 ${claudeSevenDayRemaining}%, 리셋 ${claudeSevenDayReset}`,
    `GPT 5시간 ${gptFiveHourRemaining === null ? "확인 불가" : `${gptFiveHourRemaining}%`}, 리셋 ${gptFiveHourReset}`,
    `GPT 1주 ${gptSevenDayRemaining === null ? "확인 불가" : `${gptSevenDayRemaining}%`}, 리셋 ${gptSevenDayReset}`,
  ].join(", ");

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      data-testid="chat-token-gauge"
      className={cn(
        "hidden min-w-[21rem] items-stretch gap-1.5 rounded-md border px-2 py-1 text-[10px] text-muted-foreground sm:flex",
        className,
      )}
      aria-label={ariaLabel}
    >
      <div className="grid min-w-0 flex-1 grid-cols-2 gap-1">
        <RemainingRow
          provider="Claude"
          periodLabel="5시간"
          value={claudeFiveHourRemaining}
          reset={claudeFiveHourReset}
          dataAcceptance="chat-claude-token-remaining-badge"
          testId="chat-llm-gauge-claude-5h"
        />
        <RemainingRow
          provider="Claude"
          periodLabel="1주"
          value={claudeSevenDayRemaining}
          reset={claudeSevenDayReset}
          dataAcceptance="chat-claude-token-remaining-badge"
          testId="chat-llm-gauge-claude-7d"
        />
        <RemainingRow
          provider="GPT"
          periodLabel="5시간"
          value={gptFiveHourRemaining}
          reset={gptFiveHourReset}
          dataAcceptance="chat-gpt-token-remaining-badge"
          testId="chat-llm-gauge-gpt-5h"
        />
        <RemainingRow
          provider="GPT"
          periodLabel="1주"
          value={gptSevenDayRemaining}
          reset={gptSevenDayReset}
          dataAcceptance="chat-gpt-token-remaining-badge"
          testId="chat-llm-gauge-gpt-7d"
        />
      </div>
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
  );
}

function RemainingRow({
  provider,
  periodLabel,
  value,
  reset,
  dataAcceptance,
  testId,
}: {
  provider: "Claude" | "GPT";
  periodLabel: "5시간" | "1주";
  value: number | null;
  reset: string;
  dataAcceptance: string;
  testId: string;
}) {
  return (
    <div
      data-acceptance={dataAcceptance}
      data-testid={testId}
      aria-label={`${provider} ${periodLabel} 잔량 ${value === null ? "확인 불가" : `${value}%`}, 리셋 ${reset}`}
      className="min-w-0 rounded border bg-background/40 px-1.5 py-1 leading-4"
    >
      <div className="flex items-center justify-between gap-1">
        <span className="whitespace-nowrap font-medium text-foreground">{provider} {periodLabel}</span>
        <span className="tabular-nums">{value === null ? "확인 불가" : `${value}%`}</span>
      </div>
      <div className="whitespace-nowrap text-[9px] leading-3 text-muted-foreground">{`리셋 ${reset}`}</div>
    </div>
  );
}

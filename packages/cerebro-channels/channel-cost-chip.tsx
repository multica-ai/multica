"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { issueUsageOptions } from "@multica/core/issues/queries";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * FIR-39 — accumulated channel total cost chip for the top of the channel
 * header (and any issue surface that wants the running total inline). Mirrors
 * the chat SessionCostChip (FIR-31): same shape, same look, same self-hide
 * rule — but reads /api/issues/{id}/usage instead of /chat/sessions/{id}/usage
 * because channels are issues (kind='channel') and the per-issue total has
 * existed since JEH-736.
 *
 * Self-hides until the channel has logged spend so a brand-new channel never
 * shows a misleading "$0.00". Sum of the per-comment badges below renders
 * exactly this number.
 */
export function ChannelCostChip({ channelId }: { channelId: string | null }) {
  const enabled = useFlagValue("cerebro_comment_cost");
  const { data: usage } = useQuery({
    ...issueUsageOptions(channelId ?? ""),
    enabled: enabled && !!channelId,
  });

  if (!enabled || !channelId || !usage || usage.task_count <= 0) return null;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            aria-label="Channel total cost"
            className="cursor-default rounded-md bg-accent/60 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground"
          >
            cost {formatCost(usage.cost_cents)}
          </span>
        }
      />
      <TooltipContent side="bottom" className="text-xs">
        <div className="font-medium">Channel total: {formatCost(usage.cost_cents)}</div>
        <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 text-[11px] text-muted-foreground">
          <span>Input</span>
          <span className="text-right">{formatTokens(usage.total_input_tokens)}</span>
          <span>Output</span>
          <span className="text-right">{formatTokens(usage.total_output_tokens)}</span>
          <span>Cache read</span>
          <span className="text-right">{formatTokens(usage.total_cache_read_tokens)}</span>
          <span>Cache write</span>
          <span className="text-right">{formatTokens(usage.total_cache_write_tokens)}</span>
          <span>Runs</span>
          <span className="text-right">{usage.task_count}</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

// Local formatters mirror comment-cost-badge.tsx — kept local on purpose to
// avoid a circular package edge with @multica/views.
function formatCost(cents: number): string {
  const usd = cents / 100;
  if (usd === 0) return "$0.00";
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}

function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

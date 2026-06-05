"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { chatSessionUsageOptions } from "@multica/core/chat/queries";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * FIR-31 — accumulated session-total cost chip for the top of the chat.
 *
 * The pop-out chat-window already shows this via JEH-736's ChatSessionHeader;
 * this standalone chip lets the *inbox* chat header show the same running
 * total + token breakdown so cost is visible wherever a chat is viewed. The
 * per-reply badges in the footer sum to this number.
 *
 * Reads the hydrated flag value (not useFeatureFlag, which triggers the
 * root-level flags query via useWorkspaceId) so it can mount deep in a panel
 * without workspace context. Self-hides until the session has logged spend, so
 * a brand-new chat never shows a misleading "$0.00".
 */
export function SessionCostChip({ sessionId }: { sessionId: string | null }) {
  const enabled = useFlagValue("cerebro_chat_message_cost");
  const { data: usage } = useQuery({
    ...chatSessionUsageOptions(sessionId ?? ""),
    enabled: enabled && !!sessionId,
  });

  if (!enabled || !sessionId || !usage || usage.task_count <= 0) return null;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            aria-label="Session price"
            className="cursor-default rounded-md bg-accent/60 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground"
          >
            cost {formatCost(usage.cost_cents)}
          </span>
        }
      />
      <TooltipContent side="bottom" className="text-xs">
        <div className="font-medium">Session total: {formatCost(usage.cost_cents)}</div>
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

// Local formatters mirror message-cost-badge.tsx / chat-session-header.tsx —
// kept local on purpose to avoid a circular package edge with @multica/views.
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

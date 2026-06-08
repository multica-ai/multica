"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { chatSessionMessageCostsOptions } from "@multica/core/chat/queries";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { useCostFormatter } from "@multica/cerebro-display-currency/views";

/**
 * FIR-31 — per-reply spend badge shown under an assistant chat message, beside
 * "Replied in 38s". It reads the session's per-task cost map (fetched once,
 * keyed under chatKeys.messages so the existing chat:done / task:completed
 * invalidation refreshes it) and looks up this reply's own task_id.
 *
 * Renders nothing until the cost row exists — the same "hide until known" rule
 * the elapsed caption uses. A still-running reply therefore shows no badge
 * (rather than a misleading $0.00) and it fills in automatically the moment the
 * task finishes and task_usage lands. The per-reply numbers sum to the
 * session-total chip in the chat header.
 */
export function MessageCostBadge({
  sessionId,
  taskId,
}: {
  sessionId: string;
  taskId: string | null;
}) {
  // Read the hydrated store value (not useFeatureFlag, which triggers the
  // root-level flags query via useWorkspaceId) — this badge is a leaf rendered
  // deep in the message list and must not depend on workspace context.
  const enabled = useFlagValue("cerebro_chat_message_cost");
  const { formatCents } = useCostFormatter();
  const { data } = useQuery({
    ...chatSessionMessageCostsOptions(sessionId),
    enabled: enabled && !!sessionId && !!taskId,
  });

  if (!enabled || !taskId) return null;
  const cost = data?.costs.find((c) => c.task_id === taskId);
  if (!cost) return null;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="cursor-default text-xs font-medium tabular-nums text-muted-foreground">
            cost {formatCents(cost.cost_cents)}
          </span>
        }
      />
      <TooltipContent side="top" className="text-xs">
        <div className="font-medium">This reply: {formatCents(cost.cost_cents)}</div>
        <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 text-[11px] text-muted-foreground">
          {cost.model ? (
            <>
              <span>Model</span>
              <span className="text-right">{cost.model}</span>
            </>
          ) : null}
          <span>Input</span>
          <span className="text-right">{formatTokens(cost.input_tokens)}</span>
          <span>Output</span>
          <span className="text-right">{formatTokens(cost.output_tokens)}</span>
          <span>Cache read</span>
          <span className="text-right">{formatTokens(cost.cache_read_tokens)}</span>
          <span>Cache write</span>
          <span className="text-right">{formatTokens(cost.cache_write_tokens)}</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

// Cost formatting flows through @multica/cerebro-format-cost (via
// useCostFormatter) so the workspace display currency is honored. That shared
// formatter is a zero-dependency leaf package, so it does not recreate the
// @multica/views circular edge the JEH-736 note guarded against.
function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { issueCommentCostsOptions } from "@multica/core/issues/queries";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * FIR-39 — per-comment spend badge shown under an agent comment on an issue
 * or a channel message. It reads the issue's per-task cost map (fetched once,
 * keyed under issueKeys.usage() so the existing task-lifecycle invalidation
 * refreshes it) and looks up this comment's own id.
 *
 * Renders nothing until the cost row exists — a still-running run, or a
 * progress comment that is not the run's last comment, therefore shows no
 * badge (rather than $0.00 or a stale partial number). The cost lands on the
 * run's final comment so per-comment numbers reconcile with the issue total
 * already shown in the sidebar (JEH-736).
 *
 * Lives in cerebro-channels because both consumers (issue comment-card and
 * channel slack-message-view) are upstream-zone files that already import
 * cerebro packages via CEREBRO-PATCH markers; channels reuse the comment
 * table so the same query backs both surfaces.
 */
export function CommentCostBadge({
  issueId,
  commentId,
  authorType,
}: {
  issueId: string;
  commentId: string;
  authorType: string;
}) {
  // Read the hydrated store value (not useFeatureFlag, which triggers the
  // root-level flags query via useWorkspaceId) — this badge is a leaf rendered
  // deep in the comment list and must not depend on workspace context.
  const enabled = useFlagValue("cerebro_comment_cost");
  // Only agent comments carry a task — skip the network round-trip for member
  // comments. The query itself is shared per-issue so all rendered badges
  // dedupe to one fetch.
  const isAgent = authorType === "agent";
  const { data } = useQuery({
    ...issueCommentCostsOptions(issueId),
    enabled: enabled && isAgent && !!issueId && !!commentId,
  });

  if (!enabled || !isAgent) return null;
  const cost = data?.costs.find((c) => c.comment_id === commentId);
  if (!cost) return null;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="cursor-default text-xs font-medium tabular-nums text-muted-foreground">
            cost {formatCost(cost.cost_cents)}
          </span>
        }
      />
      <TooltipContent side="top" className="text-xs">
        <div className="font-medium">This run: {formatCost(cost.cost_cents)}</div>
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

// Local formatters mirror message-cost-badge.tsx — kept local on purpose so
// this component does not pull a formatter out of @multica/views and create a
// circular package edge (same reasoning as the chat badge's note).
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

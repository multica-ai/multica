"use client";

import { memo, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { ListOrdered, TriangleAlert } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { queuedPullRequestsOptions, useGitHubSettings } from "@multica/core/github";
import type { ListGitHubQueuedPullRequestsResponse } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface IssueMergeQueueIndicatorProps {
  issueId: string;
}

/**
 * "This issue's PR is waiting in a merge queue" chip for board cards.
 *
 * A queued PR is the last state before an issue's work actually lands, but it
 * is invisible on a board: the card payload carries no pull request data, and
 * GitHub reports a queued PR as an ordinary open one. So an issue sitting in
 * the In Review column looks identical whether nobody has touched its PR or it
 * is next in line to merge (BUS-231).
 *
 * Reads the one workspace-wide queued-PR query rather than fetching this
 * issue's pull requests — a board renders hundreds of cards and per-card
 * fetching would mean hundreds of requests, while the queued set is a handful
 * of PRs at most. The `select` narrows the shared response to this issue's
 * queue state, so React Query's structural sharing re-renders only the cards
 * whose own state moved when the query refetches.
 *
 * Renders nothing when the issue has no queued PR — no chrome, no placeholder.
 */
export const IssueMergeQueueIndicator = memo(function IssueMergeQueueIndicator({
  issueId,
}: IssueMergeQueueIndicatorProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const select = useCallback(
    (res: ListGitHubQueuedPullRequestsResponse) =>
      res.queued_pull_requests.find((entry) => entry.issue_id === issueId)
        ?.merge_queue_state ?? null,
    [issueId],
  );
  // Every card mounts one of these, so the poll must not run at all in a
  // workspace that has GitHub switched off.
  const github = useGitHubSettings();
  const options = queuedPullRequestsOptions(wsId);
  const { data: queueState = null } = useQuery({
    ...options,
    enabled: options.enabled && github.enabled,
    select,
  });

  if (!queueState) return null;

  // `unmergeable` is the one queue state that is not progress: GitHub is about
  // to evict the entry, so it must not read like the others.
  const blocked = queueState === "unmergeable";
  const Icon = blocked ? TriangleAlert : ListOrdered;

  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-micro",
        blocked
          ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
          : "bg-blue-500/10 text-blue-600 dark:text-blue-400",
      )}
    >
      <Icon className="h-3 w-3" />
      {blocked ? t(($) => $.card.merge_queue_blocked) : t(($) => $.card.merge_queue)}
    </span>
  );
});

"use client";

import { memo, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import type { AvatarSize } from "@multica/ui/lib/avatar-size";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { AgentActivityHoverContent } from "../../agents/components/agent-activity-hover-content";
import { useT } from "../../i18n";

interface IssueAgentActivityIndicatorProps {
  issueId: string;
  // Avatar tier. Kept very small — this is a corner-of-card cue, not a
  // primary control. Default xs (16 px) reads as a dot at typical board
  // densities while still showing the agent's face on hover-zoom.
  size?: AvatarSize;
}

/**
 * Small "is there an agent working on this issue right now" badge shown
 * in the top-right of board cards and right after the identifier in list
 * rows. Derives state from the workspace-wide agent task snapshot:
 *
 *   - has ≥1 running task  → tiny avatar stack + shimmering "Working"
 *   - 0 running, ≥1 queued → half-opacity stack + muted "Queued"
 *   - nothing               → return null (no chrome, no placeholder)
 *
 * The shimmer reuses chat's `animate-chat-text-shimmer` utility (defined
 * in packages/ui/styles/base.css). Earlier iterations layered a brand
 * ring + opacity pulse around the avatars; both read as nervous on a
 * dense board. Moving the "alive" signal onto the label keeps the
 * avatars themselves still and lets the cue ride a piece of text the
 * user can already read.
 *
 * Hover opens AgentActivityHoverContent which lists every active task
 * with status dot + duration. No link rows — the card itself is the
 * navigation target for issue detail.
 *
 * Re-renders on every snapshot invalidation (WS task:* events drive it
 * via use-realtime-sync). 30s staleTime is the offline fallback only.
 */
export const IssueAgentActivityIndicator = memo(function IssueAgentActivityIndicator({
  issueId,
  size = "xs",
}: IssueAgentActivityIndicatorProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const { runningTasks, queuedTasks, agentIds, opacity } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    for (const task of snapshot) {
      if (task.issue_id !== issueId) continue;
      if (task.status === "running") running.push(task);
      else if (
        task.status === "queued" ||
        task.status === "dispatched" ||
        // waiting_local_directory is the daemon-parked variant of "queued"
        // — the agent is still actively waiting on a path lock, so it
        // belongs in the active hover stack rather than dropping out.
        task.status === "waiting_local_directory"
      )
        queued.push(task);
      // Terminal statuses are intentionally ignored — they belong on the
      // issue history, not the live indicator.
    }
    // Stack heads: prefer running. If 0 running, fall back to queued.
    // Each case is visually distinct (running gets shimmer, queued gets
    // muted text) so the indicator always offers a face to hover.
    const primary = running.length > 0 ? running : queued;
    const uniqueAgents = [...new Set(primary.map((t) => t.agent_id))];
    return {
      runningTasks: running,
      queuedTasks: queued,
      agentIds: uniqueAgents,
      opacity: (running.length > 0 ? "full" : "half") as "full" | "half",
    };
  }, [snapshot, issueId]);

  if (agentIds.length === 0) return null;
  const hoverTasks = [...runningTasks, ...queuedTasks];
  const isRunning = opacity === "full";

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-1" />
        }
      >
        <AgentAvatarStack
          agentIds={agentIds}
          size={size}
          opacity={opacity}
          max={3}
        />
        <span
          className={cn(
            "text-[10px] leading-none",
            isRunning
              ? "animate-chat-text-shimmer"
              : "text-muted-foreground",
          )}
        >
          {isRunning
            ? t(($) => $.agent_activity.status_running)
            : t(($) => $.agent_activity.status_queued)}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="end" className="w-72">
        <AgentActivityHoverContent tasks={hoverTasks} />
      </HoverCardContent>
    </HoverCard>
  );
});

"use client";

import { memo, useCallback, useMemo } from "react";
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
import { selectIssueTasks, type IssueTaskGroups } from "../surface/activity";
import { useT } from "../../i18n";

const EMPTY_GROUPS: IssueTaskGroups = { running: [], queued: [] };

interface IssueAgentActivityIndicatorProps {
  issueId: string;
  // Avatar tier. Kept very small — this is a corner-of-card cue, not a
  // primary control. Default xs (16 px) reads as a dot at typical board
  // densities while still showing the agent's face on hover-zoom.
  size?: AvatarSize;
  // Dense metadata rows such as Inbox already have a primary actor avatar.
  // Use a quiet status token there instead of repeating agent identity and
  // running a continuous shimmer beside every timestamp.
  variant?: "avatars" | "status";
}

/**
 * Small "is there an agent working on this issue right now" badge shown
 * in the top-right of board cards and right after the identifier in list
 * rows. Derives state from the workspace-wide agent task snapshot:
 *
 *   - has ≥1 running task  → "Working" presentation for the selected variant
 *   - 0 running, ≥1 queued → muted "Queued" presentation
 *   - nothing               → return null (no chrome, no placeholder)
 *
 * The shimmer reuses chat's `animate-chat-text-shimmer` utility (defined
 * in packages/ui/styles/base.css). Earlier iterations layered a brand
 * ring + opacity pulse around the avatars; both read as nervous on a
 * dense board. Moving the "alive" signal onto the label keeps the
 * avatars themselves still and lets the cue ride a piece of text the
 * user can already read.
 *
 * Inbox uses the `status` variant: a static semantic dot plus a regular
 * 12 px label. Its primary avatar already identifies the notification actor,
 * so another tiny face is visually ambiguous; removing the perpetual shimmer
 * also keeps a frequently scanned list calm. The hover card still exposes the
 * exact agents and tasks on demand.
 *
 * Hover opens AgentActivityHoverContent which lists every active task
 * with status dot + duration. No link rows — the card itself is the
 * navigation target for issue detail.
 *
 * Subscribes to the one shared workspace snapshot query but narrows it to
 * this issue's tasks with a `select`. React Query's structural sharing keeps
 * that selected value referentially stable when this issue's tasks are
 * unchanged, so a snapshot invalidation (WS task:* events, driven by
 * use-realtime-sync) only re-renders the rows whose own tasks actually moved
 * — not the whole list. This is the de-amplification that keeps large issue
 * lists cheap when agents are busy (MUL-4474). 30s staleTime is the offline
 * fallback only.
 */
export const IssueAgentActivityIndicator = memo(function IssueAgentActivityIndicator({
  issueId,
  size = "xs",
  variant = "avatars",
}: IssueAgentActivityIndicatorProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const select = useCallback(
    (snapshot: AgentTask[]) => selectIssueTasks(snapshot, issueId),
    [issueId],
  );
  const { data: groups = EMPTY_GROUPS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    select,
  });

  const { agentIds, opacity } = useMemo(() => {
    // Stack heads: prefer running. If 0 running, fall back to queued.
    // Each case is visually distinct (running gets shimmer, queued gets
    // muted text) so the indicator always offers a face to hover.
    const primary = groups.running.length > 0 ? groups.running : groups.queued;
    const uniqueAgents = [...new Set(primary.map((t) => t.agent_id))];
    return {
      agentIds: uniqueAgents,
      opacity: (groups.running.length > 0 ? "full" : "half") as "full" | "half",
    };
  }, [groups]);

  if (agentIds.length === 0) return null;
  const hoverTasks = [...groups.running, ...groups.queued];
  const isRunning = opacity === "full";
  const label = isRunning
    ? t(($) => $.agent_activity.status_running)
    : t(($) => $.agent_activity.status_queued);
  const isStatusVariant = variant === "status";

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <span
            className={cn(
              "inline-flex shrink-0 items-center gap-1",
              isStatusVariant && "text-xs leading-4 text-muted-foreground",
            )}
          />
        }
      >
        {isStatusVariant ? (
          <>
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                isRunning ? "bg-brand" : "bg-muted-foreground/50",
              )}
            />
            <span>{label}</span>
          </>
        ) : (
          <>
            <AgentAvatarStack
              agentIds={agentIds}
              size={size}
              opacity={opacity}
              max={3}
            />
            <span
              className={cn(
                "text-[10px] leading-4",
                isRunning
                  ? "animate-chat-text-shimmer"
                  : "text-muted-foreground",
              )}
            >
              {label}
            </span>
          </>
        )}
      </HoverCardTrigger>
      <HoverCardContent align="end" className="w-72">
        <AgentActivityHoverContent tasks={hoverTasks} />
      </HoverCardContent>
    </HoverCard>
  );
});

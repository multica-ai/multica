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
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { AgentActivityHoverContent } from "../../agents/components/agent-activity-hover-content";
import { selectIssuesTasks, type IssueTaskGroups } from "../surface/activity";
import { useT } from "../../i18n";

const EMPTY_GROUPS: IssueTaskGroups = { running: [], queued: [] };

interface SubIssuesAgentWorkingChipProps {
  /**
   * Sub-issue ids to aggregate over. Callers must pass a memoized array —
   * the id Set (and with it the query select) is rebuilt on identity change.
   */
  issueIds: readonly string[];
}

/**
 * Aggregate "N agents working" chip for the sub-issues header in issue
 * detail (multica#5825). The per-row IssueAgentActivityIndicator answers
 * "which sub-issue is being worked on"; this chip answers "how many agents
 * are on this parent's children right now" without scanning the rows — and
 * keeps that signal visible while the list is collapsed.
 *
 * Same data path as the row indicators: the one shared workspace agent-task
 * snapshot, narrowed with a select over the children's ids so only changes
 * to their own tasks re-render the header (see selectIssuesTasks).
 *
 *   - ≥1 running task → avatar stack + shimmering "N agents working"
 *   - queued only     → half-opacity stack + muted "N agents queued"
 *   - nothing         → null (no chrome, matching the row indicator)
 *
 * N counts unique agents, not tasks, matching the workspace chip whose
 * locale strings this reuses (`chip_agents_working` / `hover_header_queued`
 * ship in every locale already). Hovering lists each active task via the
 * shared activity card; default open delay is fine here — this is one
 * header chip the user aims at, not a per-row cue (cf. the 900ms rationale
 * in issue-agent-activity-indicator.tsx).
 */
export const SubIssuesAgentWorkingChip = memo(function SubIssuesAgentWorkingChip({
  issueIds,
}: SubIssuesAgentWorkingChipProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const idSet = useMemo(() => new Set(issueIds), [issueIds]);
  const select = useCallback(
    (snapshot: AgentTask[]) => selectIssuesTasks(snapshot, idSet),
    [idSet],
  );
  const { data: groups = EMPTY_GROUPS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    select,
  });

  const { agentIds, isRunning } = useMemo(() => {
    // Prefer running agents for the stack; fall back to queued so a
    // queued-only state still offers faces to hover.
    const primary = groups.running.length > 0 ? groups.running : groups.queued;
    return {
      agentIds: [...new Set(primary.map((task) => task.agent_id))],
      isRunning: groups.running.length > 0,
    };
  }, [groups]);

  if (agentIds.length === 0) return null;

  const hoverTasks = [...groups.running, ...groups.queued];

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5" />
        }
      >
        <AgentAvatarStack
          agentIds={agentIds}
          size="xs"
          opacity={isRunning ? "full" : "half"}
          max={3}
        />
        <span
          className={cn(
            "text-[11px] leading-none tabular-nums font-medium",
            isRunning ? "animate-chat-text-shimmer" : "text-muted-foreground",
          )}
        >
          {isRunning
            ? t(($) => $.agent_activity.chip_agents_working, {
                count: agentIds.length,
              })
            : t(($) => $.agent_activity.hover_header_queued, {
                count: agentIds.length,
              })}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        <AgentActivityHoverContent tasks={hoverTasks} />
      </HoverCardContent>
    </HoverCard>
  );
});

"use client";

import { memo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { WorkingAgentsHoverContent } from "./workspace-agent-working-chip";
import { useT } from "../../i18n";

interface SubIssuesAgentWorkingChipProps {
  /** Parent issue whose direct children this chip aggregates over. */
  parentIssueId: string;
}

/**
 * Aggregate "N agents working" chip for the sub-issues header in issue
 * detail (multica#5825). The per-row IssueAgentActivityIndicator answers
 * "which sub-issue is being worked on"; this chip answers "how many agents
 * are on this parent's children right now" without scanning the rows — and
 * keeps that signal visible while the list is collapsed.
 *
 * It reads the same /api/working-agents projection as the Issues list header,
 * narrowed with `parent`. That is deliberate: a header count is a claim about
 * a scope, so the server owns both the scope and the arithmetic, exactly as
 * it does for the Issues list. Deriving it here from the workspace task
 * snapshot would put a second definition of "working" in the client, and the
 * count and the hover body would each have to re-derive it.
 *
 * Row indicators keep reading the snapshot — one shared query sliced per row
 * is the right shape for a per-row cue, and a stale row decoration costs
 * nothing. A header number is the opposite: it has to be authoritative.
 */
export const SubIssuesAgentWorkingChip = memo(
  function SubIssuesAgentWorkingChip({
    parentIssueId,
  }: SubIssuesAgentWorkingChipProps) {
    const { t } = useT("issues");
    const wsId = useWorkspaceId();
    const { data: agents = [] } = useQuery(
      workspaceWorkingAgentsOptions(wsId, "issue", undefined, parentIssueId),
    );

    if (agents.length === 0) return null;

    const agentIds = agents.map((agent) => agent.id);

    return (
      <HoverCard>
        <HoverCardTrigger
          render={
            <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5" />
          }
        >
          <AgentAvatarStack agentIds={agentIds} size="xs" max={3} />
          <span className="animate-chat-text-shimmer text-[11px] font-medium leading-none tabular-nums">
            {t(($) => $.agent_activity.chip_agents_working, {
              count: agentIds.length,
            })}
          </span>
        </HoverCardTrigger>
        <HoverCardContent align="start" className="w-72">
          <WorkingAgentsHoverContent agents={agents} />
        </HoverCardContent>
      </HoverCard>
    );
  },
);

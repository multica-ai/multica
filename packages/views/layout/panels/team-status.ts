"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";

const EMPTY_AGENTS: Agent[] = [];

/**
 * Queued work on a runtime that is not online cannot start until the runtime
 * comes back — the same "stuck" reading the agent presence chip renders in
 * amber. On an online runtime a queued task is just waiting to be claimed.
 */
export function isAgentStuck(detail: AgentPresenceDetail | undefined): boolean {
  return !!detail && detail.availability !== "online" && detail.queuedCount > 0;
}

/** Live (non-archived) agents bucketed by what the team panel surfaces. */
export function useTeamAgentStatus(wsId: string | undefined) {
  const { data: agents = EMPTY_AGENTS } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { byAgent } = useWorkspacePresenceMap(wsId);
  return useMemo(() => {
    const live = agents.filter((agent) => !agent.archived_at);
    const stuck: Agent[] = [];
    const working: Agent[] = [];
    for (const agent of live) {
      const detail = byAgent.get(agent.id);
      if (isAgentStuck(detail)) stuck.push(agent);
      else if (detail?.workload === "working") working.push(agent);
    }
    return { agents: live, stuck, working, byAgent };
  }, [agents, byAgent]);
}

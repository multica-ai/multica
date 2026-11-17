"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { isAgentRuntimeBound, useWorkspacePresenceMap, type AgentAvailability } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { canAssignAgentToIssue, useCurrentMember } from "@multica/core/permissions";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, MemberWithUser, Squad } from "@multica/core/types";

export interface AgentEntry {
  agent: Agent;
  /** Whether the viewer may hand this agent work right now. */
  assignable: boolean;
  /** Why not, when the permission rule says no; runtime problems are `runtimeBound`. */
  deniedReason: string | null;
  runtimeBound: boolean;
  availability: AgentAvailability | null;
  runtimeLabel: string | null;
}

export interface SquadEntry {
  squad: Squad;
  /** A squad runs through its leader, so it is runnable when the leader is. */
  runnable: boolean;
}

const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];
const EMPTY_MEMBERS: MemberWithUser[] = [];

/**
 * Agents, squads and members as the palette shows them: archived ones out,
 * each agent annotated with whether the viewer can assign it and where it
 * runs. One pass shared by search, the assignee submenu and delegation.
 */
export function useActorDirectory(enabled: boolean) {
  const wsId = useWorkspaceId();
  const { userId, role } = useCurrentMember(wsId);
  const { data: agentsData = EMPTY_AGENTS } = useQuery({ ...agentListOptions(wsId), enabled });
  const { data: squadsData = EMPTY_SQUADS } = useQuery({ ...squadListOptions(wsId), enabled });
  const { data: members = EMPTY_MEMBERS } = useQuery({ ...memberListOptions(wsId), enabled });
  const { data: runtimes } = useQuery({ ...runtimeListOptions(wsId), enabled });
  const { byAgent } = useWorkspacePresenceMap(enabled ? wsId : undefined);

  const agents = useMemo<AgentEntry[]>(() => {
    const runtimeById = new Map((runtimes ?? []).map((runtime) => [runtime.id, runtime]));
    return agentsData
      .filter((agent) => !agent.archived_at)
      .map((agent) => {
        const decision = canAssignAgentToIssue(agent, { userId, role });
        const runtimeBound = isAgentRuntimeBound(agent);
        const runtime = agent.runtime_id ? runtimeById.get(agent.runtime_id) : undefined;
        return {
          agent,
          assignable: decision.allowed && runtimeBound,
          deniedReason: decision.allowed ? null : decision.message ?? null,
          runtimeBound,
          availability: byAgent.get(agent.id)?.availability ?? null,
          runtimeLabel: runtime ? runtimeDisplayLabel(runtime) : null,
        };
      });
  }, [agentsData, byAgent, role, runtimes, userId]);

  const squads = useMemo<SquadEntry[]>(() => {
    const runnable = new Set(agents.filter((entry) => entry.runtimeBound).map((entry) => entry.agent.id));
    return squadsData
      .filter((squad) => !squad.archived_at)
      .map((squad) => ({ squad, runnable: runnable.has(squad.leader_id) }));
  }, [agents, squadsData]);

  return useMemo(() => ({ agents, squads, members }), [agents, squads, members]);
}

import type {
  AgentAvailability,
  AgentPresenceDetail,
} from "@multica/core/agents";

export type AgentStatusFilter = "all" | AgentAvailability | "working";

export interface AgentStatusFilterCounts {
  availability: Record<AgentAvailability, number>;
  working: number;
}

interface AgentIdOnly {
  id: string;
}

export function matchesAgentStatusFilter(
  agentId: string,
  presenceMap: ReadonlyMap<string, AgentPresenceDetail>,
  filter: AgentStatusFilter,
): boolean {
  if (filter === "all") return true;

  const detail = presenceMap.get(agentId);
  if (!detail) return false;

  if (filter === "working") {
    return detail.workload === "working";
  }

  return detail.availability === filter;
}

export function countAgentStatusFilters(
  agents: readonly AgentIdOnly[],
  presenceMap: ReadonlyMap<string, AgentPresenceDetail>,
): AgentStatusFilterCounts {
  const availability: Record<AgentAvailability, number> = {
    online: 0,
    unstable: 0,
    offline: 0,
  };
  let working = 0;

  for (const agent of agents) {
    const detail = presenceMap.get(agent.id);
    if (!detail) continue;

    availability[detail.availability] += 1;
    if (detail.workload === "working") {
      working += 1;
    }
  }

  return { availability, working };
}

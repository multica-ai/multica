import type { Agent, Autopilot, Squad } from "../types";

export interface AutopilotMineFilterContext {
  currentUserId: string | null | undefined;
  ownedAgentIds: Iterable<string>;
  squads?: readonly Pick<Squad, "id" | "leader_id">[];
}

interface AutopilotMineFilterLookup {
  currentUserId: string;
  ownedAgentIds: ReadonlySet<string>;
  squadLeaderById: ReadonlyMap<string, string>;
}

function buildMineFilterLookup(
  context: AutopilotMineFilterContext,
): AutopilotMineFilterLookup | null {
  const { currentUserId } = context;
  if (!currentUserId) return null;
  return {
    currentUserId,
    ownedAgentIds: new Set(context.ownedAgentIds),
    squadLeaderById: new Map(
      (context.squads ?? []).map((squad) => [squad.id, squad.leader_id]),
    ),
  };
}

function isMineAutopilotWithLookup(
  autopilot: Pick<
    Autopilot,
    | "assignee_type"
    | "assignee_id"
    | "created_by_type"
    | "created_by_id"
  >,
  lookup: AutopilotMineFilterLookup,
): boolean {
  const { currentUserId, ownedAgentIds, squadLeaderById } = lookup;

  if (
    autopilot.created_by_type === "member" &&
    autopilot.created_by_id === currentUserId
  ) {
    return true;
  }
  if (
    autopilot.created_by_type === "agent" &&
    ownedAgentIds.has(autopilot.created_by_id)
  ) {
    return true;
  }
  if (
    autopilot.assignee_type === "agent" &&
    ownedAgentIds.has(autopilot.assignee_id)
  ) {
    return true;
  }
  if (autopilot.assignee_type === "squad") {
    const leaderId = squadLeaderById.get(autopilot.assignee_id);
    return !!leaderId && ownedAgentIds.has(leaderId);
  }
  return false;
}

export function isMineAutopilot(
  autopilot: Pick<
    Autopilot,
    | "assignee_type"
    | "assignee_id"
    | "created_by_type"
    | "created_by_id"
  >,
  context: AutopilotMineFilterContext,
): boolean {
  const lookup = buildMineFilterLookup(context);
  return !!lookup && isMineAutopilotWithLookup(autopilot, lookup);
}

export function filterMineAutopilots<T extends Autopilot>(
  autopilots: readonly T[],
  context: AutopilotMineFilterContext,
): T[] {
  const lookup = buildMineFilterLookup(context);
  if (!lookup) return [];
  return autopilots.filter((autopilot) =>
    isMineAutopilotWithLookup(autopilot, lookup),
  );
}

export function ownedAgentIdsForUser(
  agents: readonly Pick<Agent, "id" | "owner_id">[],
  currentUserId: string | null | undefined,
): string[] {
  if (!currentUserId) return [];
  return agents
    .filter((agent) => agent.owner_id === currentUserId)
    .map((agent) => agent.id);
}

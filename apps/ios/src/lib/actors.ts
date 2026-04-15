import type {
  Agent,
  IssueAssigneeType,
  MemberWithUser,
} from "@multica/core/types";

export type ActorInfo = {
  name: string;
  avatarUrl: string | null;
  isAgent: boolean;
};

export function createActorResolver(
  members: MemberWithUser[],
  agents: Agent[],
) {
  const memberMap = new Map<string, ActorInfo>();
  const agentMap = new Map<string, ActorInfo>();

  for (const member of members) {
    memberMap.set(member.user_id, {
      name: member.name,
      avatarUrl: member.avatar_url,
      isAgent: false,
    });
  }

  for (const agent of agents) {
    agentMap.set(agent.id, {
      name: agent.name,
      avatarUrl: agent.avatar_url,
      isAgent: true,
    });
  }

  return (actorType: IssueAssigneeType | string | null, actorId: string | null) => {
    if (!actorType || !actorId) return null;
    return actorType === "agent" ? agentMap.get(actorId) ?? null : memberMap.get(actorId) ?? null;
  };
}

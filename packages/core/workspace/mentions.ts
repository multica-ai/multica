import type {
  Agent,
  Issue,
  IssueStatus,
  MemberWithUser,
  MentionFrequencyEntry,
} from "../types";

export type WorkspaceMentionTarget = {
  id: string;
  label: string;
  type: "all" | "member" | "agent" | "issue";
  description?: string;
  status?: IssueStatus;
};

export type MentionPermissionContext = {
  userId: string | null;
  role: "owner" | "admin" | "member" | null;
};

export function buildWorkspaceMentionTargets(
  members: MemberWithUser[],
  agents: Agent[],
  ctx: MentionPermissionContext,
): WorkspaceMentionTarget[] {
  return [
    { id: "all", label: "All members", type: "all" },
    ...members.map((member) => ({
      id: member.user_id,
      label: member.name,
      type: "member" as const,
    })),
    ...agents
      .filter(
        (agent) =>
          !agent.archived_at &&
          // Workspace agents are shared — always visible to all members.
          // Private agents are restricted to their owner only.
          // Legacy agents (owner_id null) remain visible to everyone.
          (agent.visibility === "workspace" ||
            agent.owner_id === null ||
            agent.owner_id === ctx.userId ||
            (!!ctx.userId && (agent.allowed_user_ids?.includes(ctx.userId) ?? false))),
      )
      .map((agent) => ({
        id: agent.id,
        label: agent.name,
        type: "agent" as const,
      })),
  ];
}

export function issueToMentionTarget(
  issue: Pick<Issue, "id" | "identifier" | "title" | "status">,
): WorkspaceMentionTarget {
  return {
    id: issue.id,
    label: issue.identifier,
    type: "issue",
    description: issue.title,
    status: issue.status,
  };
}

export function mergeMentionTargets(
  ...groups: WorkspaceMentionTarget[][]
): WorkspaceMentionTarget[] {
  const seen = new Set<string>();
  const merged: WorkspaceMentionTarget[] = [];
  for (const group of groups) {
    for (const item of group) {
      const key = `${item.type}:${item.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(item);
    }
  }
  return merged;
}

export function sortMentionTargetsByFrequency(
  targets: WorkspaceMentionTarget[],
  frequency: MentionFrequencyEntry[],
  options: { ownAgentIds?: Iterable<string> } = {},
): WorkspaceMentionTarget[] {
  const ownAgentIds = new Set(options.ownAgentIds ?? []);
  const rank = new Map(
    frequency.map((entry) => [
      `${entry.actor_type}:${entry.actor_id}`,
      {
        frequency: entry.frequency,
        lastMentionedAt: Date.parse(entry.last_mentioned_at) || 0,
      },
    ]),
  );

  return [...targets].sort((a, b) => {
    const aOwnAgent = a.type === "agent" && ownAgentIds.has(a.id);
    const bOwnAgent = b.type === "agent" && ownAgentIds.has(b.id);
    if (aOwnAgent !== bOwnAgent) return aOwnAgent ? -1 : 1;

    if (a.type === "all" || b.type === "all") {
      if (a.type === b.type) return 0;
      return a.type === "all" ? 1 : -1;
    }

    const aRank = rank.get(`${a.type}:${a.id}`);
    const bRank = rank.get(`${b.type}:${b.id}`);
    if (aRank || bRank) {
      if (!aRank) return 1;
      if (!bRank) return -1;
      if (bRank.frequency !== aRank.frequency) {
        return bRank.frequency - aRank.frequency;
      }
      return bRank.lastMentionedAt - aRank.lastMentionedAt;
    }
    return a.label.localeCompare(b.label);
  });
}

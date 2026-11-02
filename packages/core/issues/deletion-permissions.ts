import type { Agent, Issue, MemberRole } from "../types";

export function canDeleteIssue(
  issue: Pick<Issue, "creator_type" | "creator_id">,
  restricted: boolean,
  userId: string | null,
  role: MemberRole | null,
  agents: readonly Pick<Agent, "id" | "owner_id">[],
): boolean {
  if (!restricted) return true;
  if (!userId || !role) return false;
  if (role === "owner" || role === "admin") return true;
  if (issue.creator_type === "member") return issue.creator_id === userId;
  return (
    issue.creator_type === "agent" &&
    agents.some(
      (agent) => agent.id === issue.creator_id && agent.owner_id === userId,
    )
  );
}

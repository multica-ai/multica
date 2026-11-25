import type { Agent, Issue, MemberRole } from "../types";

// Mirrors the server rule. The server stays authoritative; this only decides
// whether delete affordances are offered. A guest deletes nothing, not even an
// issue it created before it became a guest.
export function canDeleteIssue(
  issue: Pick<Issue, "creator_type" | "creator_id">,
  userId: string | null,
  role: MemberRole | null,
  agents: readonly Pick<Agent, "id" | "owner_id">[],
): boolean {
  if (!userId || !role || role === "guest") return false;
  if (role === "owner" || role === "admin") return true;
  if (issue.creator_type === "member") return issue.creator_id === userId;
  return (
    issue.creator_type === "agent" &&
    agents.some(
      (agent) => agent.id === issue.creator_id && agent.owner_id === userId,
    )
  );
}

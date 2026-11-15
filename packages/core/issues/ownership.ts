import type { Agent, IssueAssigneeType } from "../types";

/**
 * Whether an issue is the viewer's own work: assigned to them, or to an agent
 * they own. Everything else they watch they are only a bystander on, which is
 * where the inbox offers "unsubscribe & archive".
 *
 * Squad assignments count as someone else's: a squad is shared, and the
 * viewer being in it does not make every issue it picks up theirs.
 */
export function isOwnIssue(
  issue: { assignee_type?: IssueAssigneeType | null; assignee_id?: string | null },
  userId: string | null | undefined,
  agents: readonly Pick<Agent, "id" | "owner_id">[],
): boolean {
  if (!userId || !issue.assignee_id) return false;
  if (issue.assignee_type === "member") return issue.assignee_id === userId;
  if (issue.assignee_type === "agent") {
    return agents.some((agent) => agent.id === issue.assignee_id && agent.owner_id === userId);
  }
  return false;
}

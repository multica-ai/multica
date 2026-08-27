import type { Agent } from "@multica/core/types";
import { canAssignAgentToIssue } from "@multica/core/permissions";

/**
 * Legacy boolean shape kept around for callers (e.g. `use-issue-actions.ts`)
 * that haven't migrated to the new `canAssignAgentToIssue` Decision API yet.
 * Internally redirects to the canonical rule so behaviour stays in sync.
 *
 * Lives in its own module so chat/landing graphs can import it without the
 * assignee picker (members/agents queries, editor pinyin, …).
 */
export function canAssignAgent(
  agent: Agent,
  userId: string | undefined,
  memberRole: string | undefined,
): boolean {
  return canAssignAgentToIssue(agent, {
    userId: userId ?? null,
    role: memberRole === "owner" || memberRole === "admin" || memberRole === "member"
      ? memberRole
      : null,
  }).allowed;
}

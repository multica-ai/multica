/**
 * Mobile-owned mirror of the boolean shim
 * `packages/views/issues/components/pickers/assignee-picker.tsx:canAssignAgent`
 * — which in turn forwards to `packages/core/permissions/rules.ts:canAssignAgentToIssue`.
 *
 * We mirror (not import) per the apps/mobile/CLAUDE.md sharing rule: only
 * `import type` from @multica/core; logic is duplicated to keep mobile
 * independent. Any rule change must be applied here too.
 *
 * Rule (mirrors backend `server/internal/handler/issue.go:1471-1490`):
 *   - Workspace-visibility agents → assignable by any workspace member
 *   - Private agents               → only owner + workspace admins/owners
 *
 * Used by the chat agent picker to filter "agents I can talk to" and by
 * NoAgentBanner to detect the all-zero state.
 */
import type { Agent } from "@multica/core/types";

type MemberRoleLike = "owner" | "admin" | "member" | null | undefined;

export function canAssignAgent(
  agent: Agent,
  userId: string | undefined | null,
  memberRole: MemberRoleLike,
): boolean {
  if (!userId) return false;

  const role: MemberRoleLike =
    memberRole === "owner" || memberRole === "admin" || memberRole === "member"
      ? memberRole
      : null;

  if (agent.visibility === "workspace") {
    return role !== null;
  }
  // visibility === "private" (or anything else — treat unknown as private,
  // which is the safer side of an enum drift).
  if (role === "owner" || role === "admin") return true;
  return agent.owner_id !== null && agent.owner_id === userId;
}

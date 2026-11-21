/**
 * The current user's role in a workspace, read from the cached member list.
 * Mirrors `useCurrentMember` / `useIsGuest` in `packages/core/permissions`.
 */
import type { MemberRole, MemberWithUser } from "@multica/core/types";

export function memberRoleOf(
  members: readonly MemberWithUser[] | undefined,
  userId: string | null | undefined,
): MemberRole | null {
  if (!members || !userId) return null;
  return members.find((m) => m.user_id === userId)?.role ?? null;
}

/**
 * A guest (MUL-9420) reads and comments on the issues of the projects shared
 * with it. It cannot create issues, change their properties, or use agents,
 * so those controls are hidden from it. False while the member list loads.
 */
export function isGuestRole(role: MemberRole | null | undefined): boolean {
  return role === "guest";
}

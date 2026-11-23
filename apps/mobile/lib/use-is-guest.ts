import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@/data/auth-store";
import { memberListOptions } from "@/data/queries/members";
import { useWorkspaceStore } from "@/data/workspace-store";
import { isGuestRole, memberRoleOf } from "@/lib/member-role";

/** Whether the current user is a guest of the current workspace (MUL-9420). */
export function useIsGuest(): boolean {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members } = useQuery(memberListOptions(wsId));
  return isGuestRole(memberRoleOf(members, userId));
}

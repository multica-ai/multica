import { useQuery } from "@tanstack/react-query";
import { canManageProjects } from "@multica/core/projects/permissions";
import { useAuthStore } from "@/data/auth-store";
import { memberListOptions } from "@/data/queries/members";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCanManageProjects(): boolean {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  return canManageProjects(members.find((member) => member.user_id === userId)?.role);
}

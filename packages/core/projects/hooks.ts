import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import { memberListOptions } from "../workspace/queries";
import { canManageProjects } from "./permissions";

export function useCanManageProjects(wsId: string): boolean {
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  return canManageProjects(members.find((member) => member.user_id === userId)?.role);
}

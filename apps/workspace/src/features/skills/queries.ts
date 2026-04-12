import { useQuery } from "@tanstack/react-query";
import type { Skill } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";
import { useWorkspaceStore } from "@/features/workspace";

export function skillDetailQueryOptions(workspaceId: string, skillId: string) {
  return {
    queryKey: queryKeys.workspace.skillDetail(workspaceId, skillId),
    queryFn: () => api.getSkill(skillId),
    staleTime: 30 * 1000,
  };
}

export function useSkillDetailQuery(skillId?: string | null) {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<Skill | null>({
    ...(workspaceId && skillId
      ? skillDetailQueryOptions(workspaceId, skillId)
      : {
          queryKey: queryKeys.workspace.skillDetail("__no_workspace__", "__missing_skill__"),
          queryFn: async () => null,
          staleTime: 30 * 1000,
        }),
    enabled: Boolean(workspaceId && skillId) && hasStoredSessionToken(),
  });
}

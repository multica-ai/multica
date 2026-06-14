import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type { IssueType } from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

/** Loads workspace issue types used to describe the shape and energy profile of work. */
export function useIssueTypesQuery(includeArchived = false) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<IssueType[]>({
    queryKey: queryKeys.issueTypes.list(workspaceId, includeArchived),
    queryFn: () => api.listIssueTypes(includeArchived),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}

/** Creates a workspace issue type and refreshes issue type caches. */
export function useCreateIssueTypeMutation(includeArchived = false) {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation({
    mutationFn: (body: {
      key: string;
      name: string;
      load_profile: string;
      color?: string;
      icon?: string;
      description?: string;
      position?: number;
    }) => api.createIssueType(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.issueTypes.list(workspaceId, includeArchived) });
    },
  });
}

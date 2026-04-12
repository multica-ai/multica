import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";
import { useWorkspaceStore } from "@/features/workspace";

const RUNTIMES_STALE_TIME = 30 * 1000;

export function runtimesQueryOptions(workspaceId: string) {
  return {
    queryKey: queryKeys.runtimes.all(workspaceId),
    queryFn: () => api.listRuntimes({ workspace_id: workspaceId }),
    staleTime: RUNTIMES_STALE_TIME,
  };
}

export function useRuntimesQuery() {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<AgentRuntime[]>({
    ...(workspaceId
      ? runtimesQueryOptions(workspaceId)
      : {
          queryKey: queryKeys.runtimes.all("__no_workspace__"),
          queryFn: async () => [] as AgentRuntime[],
          staleTime: RUNTIMES_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}

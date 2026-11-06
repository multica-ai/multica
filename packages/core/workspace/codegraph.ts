import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
export * from "./codegraph-assignments";
export * from "./git-credentials";

export function availableCodeGraphsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: ["workspaces", wsId, "code-graphs", "available", taskId],
    queryFn: () => api.availableCodeGraphs(wsId, taskId),
    staleTime: 0,
  });
}

export function useCodeGraphScope(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (
      change: { repositoryId: string } & (
        | { shared: boolean }
        | { projectId: string; assigned: boolean }
      ),
    ) =>
      "shared" in change
        ? api.setCodeGraphWorkspaceScope(
            wsId,
            change.repositoryId,
            change.shared,
          )
        : api.setCodeGraphProject(
            wsId,
            change.repositoryId,
            change.projectId,
            change.assigned,
          ),
    onSettled: () =>
      client.invalidateQueries({
        queryKey: ["workspaces", wsId, "code-graphs"],
      }),
  });
}

export type CodeGraphAssignmentChange = {
  repositoryId: string;
  agentId: string;
  assigned: boolean;
};

export function useCodeGraphAssignmentChanges(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (changes: CodeGraphAssignmentChange[]) => {
      const results = await Promise.allSettled(
        changes.map((change) =>
          change.assigned
            ? api.assignCodeGraphRepositoryToAgent(
                wsId,
                change.repositoryId,
                change.agentId,
              )
            : api.unassignCodeGraphRepositoryAgent(
                wsId,
                change.repositoryId,
                change.agentId,
              ),
        ),
      );
      return results.flatMap((result, index) =>
        result.status === "rejected"
          ? [
              {
                change: changes[index]!,
                error:
                  result.reason instanceof Error
                    ? result.reason.message
                    : String(result.reason),
              },
            ]
          : [],
      );
    },
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({
          queryKey: ["workspaces", wsId, "code-graphs"],
        }),
        client.invalidateQueries({ queryKey: ["workspaces", wsId, "agents"] }),
      ]),
  });
}

export function useCodeGraphQuery(wsId: string, repositoryId: string) {
  return useMutation({
    mutationFn: (params: Record<string, string>) =>
      api.queryCodeGraph(wsId, repositoryId, params),
  });
}

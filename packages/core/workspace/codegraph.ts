import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import {
  codeGraphRepositoriesOptions,
  codeGraphRepositoryOptions,
} from "./queries";
export * from "./codegraph-assignments";
export * from "./git-credentials";

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

export function useCodeGraphSharing(wsId: string, repositoryId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (enabled: boolean) =>
      api.setCodeGraphSharing(wsId, repositoryId, enabled),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: codeGraphRepositoryOptions(wsId, repositoryId).queryKey,
        }),
        client.invalidateQueries({
          queryKey: codeGraphRepositoriesOptions(wsId).queryKey,
        }),
      ]);
    },
  });
}

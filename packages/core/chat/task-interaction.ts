import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { TaskInteractionCommand } from "../api/task-interaction-schema";

export type { TaskInteraction, TaskInteractionCommand } from "../api/task-interaction-schema";

export const interactionKey = (workspaceId: string, taskId: string) =>
  ["task-interaction", workspaceId, taskId] as const;

export function taskInteractionOptions(workspaceId: string, taskId: string, enabled: boolean) {
  return queryOptions({
    queryKey: interactionKey(workspaceId, taskId),
    queryFn: () => api.getTaskInteraction(taskId),
    enabled: enabled && !!workspaceId && !!taskId,
    staleTime: 0,
    refetchInterval: (query) => query.state.data?.state.state === "finished" ? false : 1000,
  });
}

export function useTaskInteraction(workspaceId: string, taskId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (command: TaskInteractionCommand) => api.sendTaskInteraction(taskId, command),
    onSuccess: (record) => {
      if (record) client.setQueryData(interactionKey(workspaceId, taskId), record);
    },
    onSettled: () => client.invalidateQueries({ queryKey: interactionKey(workspaceId, taskId) }),
  });
}

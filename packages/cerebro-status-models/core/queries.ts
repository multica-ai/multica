import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  assignProjectStatusModel,
  clearProjectStatusModel,
  createStatusModel,
  deleteStatusModel,
  fetchStatusModel,
  fetchStatusModelAssignments,
  fetchStatusModels,
  updateStatusModel,
} from "./api";
import { issueKeys } from "@multica/core/issues/queries";
import type { StatusModelWriteInput } from "./types";

export const cerebroStatusModelsKeys = {
  all: (wsId: string) => ["cerebro", "status-models", wsId] as const,
  list: (wsId: string) => [...cerebroStatusModelsKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...cerebroStatusModelsKeys.all(wsId), "detail", id] as const,
  assignments: (wsId: string) => [...cerebroStatusModelsKeys.all(wsId), "assignments"] as const,
};

export function cerebroStatusModelsListOptions(wsId: string) {
  return queryOptions({
    queryKey: cerebroStatusModelsKeys.list(wsId),
    queryFn: fetchStatusModels,
    enabled: !!wsId,
    staleTime: 15 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function cerebroStatusModelDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cerebroStatusModelsKeys.detail(wsId, id),
    queryFn: () => fetchStatusModel(id),
    enabled: !!wsId && !!id,
    staleTime: 30 * 1000,
  });
}

export function cerebroStatusModelAssignmentsOptions(wsId: string) {
  return queryOptions({
    queryKey: cerebroStatusModelsKeys.assignments(wsId),
    queryFn: fetchStatusModelAssignments,
    enabled: !!wsId,
    staleTime: 15 * 1000,
  });
}

// Mutations invalidate the list (and assignments, where project links change)
// on settle so the settings page and overview panel re-sync from the server.

export function useCreateStatusModelMutation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: StatusModelWriteInput) => createStatusModel(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.list(wsId) });
    },
  });
}

export function useUpdateStatusModelMutation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: StatusModelWriteInput }) =>
      updateStatusModel(id, payload),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.list(wsId) });
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.detail(wsId, id) });
    },
  });
}

export function useDeleteStatusModelMutation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteStatusModel(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.list(wsId) });
    },
  });
}

export function useAssignProjectStatusModelMutation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      projectId,
      statusModelId,
      mapping,
    }: {
      projectId: string;
      statusModelId: string;
      mapping?: Record<string, string>;
    }) => assignProjectStatusModel(projectId, statusModelId, mapping),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.assignments(wsId) });
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.list(wsId) });
      // Existing issues were re-pinned server-side — refresh issue lists/boards
      // so the new custom-status pins show without a manual reload.
      queryClient.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useClearProjectStatusModelMutation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (projectId: string) => clearProjectStatusModel(projectId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.assignments(wsId) });
      queryClient.invalidateQueries({ queryKey: cerebroStatusModelsKeys.list(wsId) });
    },
  });
}

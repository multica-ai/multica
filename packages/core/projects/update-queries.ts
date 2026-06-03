import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type {
  ListProjectUpdatesResponse,
  ProjectUpdate,
  CreateProjectUpdateRequest,
  UpdateProjectUpdateRequest,
} from "../types/project";

export const projectUpdateKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "updates"] as const,
};

export function projectUpdatesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectUpdateKeys.list(wsId, projectId),
    queryFn: () => api.listProjectUpdates(projectId),
    select: (data: ListProjectUpdatesResponse) => data.updates,
  });
}

export function useCreateProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateProjectUpdateRequest) =>
      api.createProjectUpdate(projectId, data),
    onSuccess: (created: ProjectUpdate) => {
      qc.setQueryData<ListProjectUpdatesResponse>(
        projectUpdateKeys.list(wsId, projectId),
        (old) =>
          old && !old.updates.some((u) => u.id === created.id)
            ? {
                ...old,
                updates: [created, ...old.updates],
                total: old.total + 1,
              }
            : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectUpdateKeys.list(wsId, projectId),
      });
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}

export function useUpdateProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      updateId,
      ...data
    }: { updateId: string } & UpdateProjectUpdateRequest) =>
      api.updateProjectUpdate(projectId, updateId, data),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectUpdateKeys.list(wsId, projectId),
      });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}

export function useDeleteProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (updateId: string) =>
      api.deleteProjectUpdate(projectId, updateId),
    onMutate: (updateId: string) => {
      const key = projectUpdateKeys.list(wsId, projectId);
      const prev = qc.getQueryData<ListProjectUpdatesResponse>(key);
      qc.setQueryData<ListProjectUpdatesResponse>(key, (old) =>
        old
          ? {
              ...old,
              updates: old.updates.filter((u) => u.id !== updateId),
              total: Math.max(0, old.total - 1),
            }
          : old,
      );
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(projectUpdateKeys.list(wsId, projectId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectUpdateKeys.list(wsId, projectId),
      });
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}

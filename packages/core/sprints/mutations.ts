import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { sprintKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type { Sprint } from "../types";

export function useCreateSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { name: string; goal?: string; start_date: string; end_date: string }) =>
      api.createSprint(data),
    onSuccess: (newSprint) => {
      qc.setQueryData<{ sprints: Sprint[]; total: number }>(sprintKeys.list(wsId), (old) =>
        old && !old.sprints.some((s) => s.id === newSprint.id)
          ? { sprints: [...old.sprints, newSprint], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId) });
    },
  });
}

export function useUpdateSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & Record<string, unknown>) =>
      api.updateSprint(id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: sprintKeys.list(wsId) });
      const prevList = qc.getQueryData<{ sprints: Sprint[]; total: number }>(sprintKeys.list(wsId));
      const prevDetail = qc.getQueryData<Sprint>(sprintKeys.detail(wsId, id));
      qc.setQueryData<{ sprints: Sprint[]; total: number }>(sprintKeys.list(wsId), (old) =>
        old ? { ...old, sprints: old.sprints.map((s) => (s.id === id ? { ...s, ...data } : s)) } : old,
      );
      qc.setQueryData<Sprint>(sprintKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data } : old,
      );
      return { prevList, prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(sprintKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(sprintKeys.detail(wsId, ctx.id), ctx.prevDetail);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: sprintKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId) });
    },
  });
}

export function useDeleteSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteSprint(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: sprintKeys.list(wsId) });
      const prevList = qc.getQueryData<{ sprints: Sprint[]; total: number }>(sprintKeys.list(wsId));
      qc.setQueryData<{ sprints: Sprint[]; total: number }>(sprintKeys.list(wsId), (old) =>
        old ? { sprints: old.sprints.filter((s) => s.id !== id), total: old.total - 1 } : old,
      );
      qc.removeQueries({ queryKey: sprintKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(sprintKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId) });
    },
  });
}

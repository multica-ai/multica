import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { autopilotKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateAutopilotRequest,
  UpdateAutopilotRequest,
  ListAutopilotsResponse,
  GetAutopilotResponse,
  CreateAutopilotTriggerRequest,
  UpdateAutopilotTriggerRequest,
} from "../types";

export function useCreateAutopilot() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateAutopilotRequest) => api.createAutopilot(data),
    onSuccess: (newAutopilot) => {
      qc.setQueryData<ListAutopilotsResponse>(autopilotKeys.list(wsId), (old) =>
        old && !old.autopilots.some((a) => a.id === newAutopilot.id)
          ? { ...old, autopilots: [...old.autopilots, newAutopilot], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

export function useUpdateAutopilot() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateAutopilotRequest) =>
      api.updateAutopilot(id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: autopilotKeys.list(wsId) });
      const prevList = qc.getQueryData<ListAutopilotsResponse>(autopilotKeys.list(wsId));
      const prevDetail = qc.getQueryData<GetAutopilotResponse>(autopilotKeys.detail(wsId, id));
      qc.setQueryData<ListAutopilotsResponse>(autopilotKeys.list(wsId), (old) =>
        old ? { ...old, autopilots: old.autopilots.map((a) => (a.id === id ? { ...a, ...data } : a)) } : old,
      );
      qc.setQueryData<GetAutopilotResponse>(autopilotKeys.detail(wsId, id), (old) =>
        old ? { ...old, autopilot: { ...old.autopilot, ...data } } : old,
      );
      return { prevList, prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(autopilotKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(autopilotKeys.detail(wsId, ctx.id), ctx.prevDetail);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

export function useDeleteAutopilot() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteAutopilot(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: autopilotKeys.list(wsId) });
      const prevList = qc.getQueryData<ListAutopilotsResponse>(autopilotKeys.list(wsId));
      qc.setQueryData<ListAutopilotsResponse>(autopilotKeys.list(wsId), (old) =>
        old ? { ...old, autopilots: old.autopilots.filter((a) => a.id !== id), total: old.total - 1 } : old,
      );
      qc.removeQueries({ queryKey: autopilotKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(autopilotKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}

export function useTriggerAutopilot() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.triggerAutopilot(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: autopilotKeys.runs(wsId, id) });
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, id) });
    },
  });
}

export function useCreateAutopilotTrigger() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ autopilotId, ...data }: { autopilotId: string } & CreateAutopilotTriggerRequest) =>
      api.createAutopilotTrigger(autopilotId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, vars.autopilotId) });
    },
  });
}

export function useUpdateAutopilotTrigger() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ autopilotId, triggerId, ...data }: { autopilotId: string; triggerId: string } & UpdateAutopilotTriggerRequest) =>
      api.updateAutopilotTrigger(autopilotId, triggerId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, vars.autopilotId) });
    },
  });
}

export function useDeleteAutopilotTrigger() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ autopilotId, triggerId }: { autopilotId: string; triggerId: string }) =>
      api.deleteAutopilotTrigger(autopilotId, triggerId),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: autopilotKeys.detail(wsId, vars.autopilotId) });
    },
  });
}

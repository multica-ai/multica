import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { integrationKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  Integration,
  CreateIntegrationRequest,
  UpdateIntegrationRequest,
} from "../types";

export function useCreateIntegration() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateIntegrationRequest) => api.createIntegration(data),
    onSuccess: (newIntegration) => {
      qc.setQueryData<Integration[]>(integrationKeys.list(wsId), (old) =>
        old ? [...old, newIntegration] : [newIntegration],
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: integrationKeys.list(wsId) });
    },
  });
}

export function useUpdateIntegration() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIntegrationRequest) =>
      api.updateIntegration(id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: integrationKeys.list(wsId) });
      const prev = qc.getQueryData<Integration[]>(integrationKeys.list(wsId));
      qc.setQueryData<Integration[]>(integrationKeys.list(wsId), (old) =>
        old
          ? old.map((i) => (i.id === id ? { ...i, ...data } : i))
          : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(integrationKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: integrationKeys.list(wsId) });
    },
  });
}

export function useDeleteIntegration() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteIntegration(id),
    onMutate: (id) => {
      qc.cancelQueries({ queryKey: integrationKeys.list(wsId) });
      const prev = qc.getQueryData<Integration[]>(integrationKeys.list(wsId));
      qc.setQueryData<Integration[]>(integrationKeys.list(wsId), (old) =>
        old ? old.filter((i) => i.id !== id) : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(integrationKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: integrationKeys.list(wsId) });
    },
  });
}

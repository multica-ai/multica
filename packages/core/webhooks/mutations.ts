import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiClient } from "../api";
import { webhookKeys } from "./queries";
import type { CreateWebhookEndpointRequest, UpdateWebhookEndpointRequest, WebhookEndpoint } from "../types";

export function useCreateWebhookEndpoint(wsId: string) {
  const qc = useQueryClient();
  const api = useApiClient();
  return useMutation({
    mutationFn: (data: CreateWebhookEndpointRequest) => api.createWebhookEndpoint(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: webhookKeys.list(wsId) });
    },
  });
}

export function useUpdateWebhookEndpoint(wsId: string) {
  const qc = useQueryClient();
  const api = useApiClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateWebhookEndpointRequest }) =>
      api.updateWebhookEndpoint(id, data),
    onMutate: async ({ id, data }) => {
      await qc.cancelQueries({ queryKey: webhookKeys.list(wsId) });
      const prev = qc.getQueryData<WebhookEndpoint[]>(webhookKeys.list(wsId));
      qc.setQueryData<WebhookEndpoint[]>(webhookKeys.list(wsId), (old) =>
        old?.map((ep) => (ep.id === id ? { ...ep, ...data } : ep)),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(webhookKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: webhookKeys.list(wsId) });
    },
  });
}

export function useDeleteWebhookEndpoint(wsId: string) {
  const qc = useQueryClient();
  const api = useApiClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteWebhookEndpoint(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: webhookKeys.list(wsId) });
      const prev = qc.getQueryData<WebhookEndpoint[]>(webhookKeys.list(wsId));
      qc.setQueryData<WebhookEndpoint[]>(webhookKeys.list(wsId), (old) =>
        old?.filter((ep) => ep.id !== id),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(webhookKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: webhookKeys.list(wsId) });
    },
  });
}

export function useTestWebhookEndpoint() {
  const api = useApiClient();
  return useMutation({
    mutationFn: (id: string) => api.testWebhookEndpoint(id),
  });
}

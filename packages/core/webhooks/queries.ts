import { queryOptions } from "@tanstack/react-query";
import { useApiClient } from "../api";

export const webhookKeys = {
  all: (wsId: string) => ["webhooks", wsId] as const,
  list: (wsId: string) => [...webhookKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...webhookKeys.all(wsId), "detail", id] as const,
  deliveries: (wsId: string, id: string) => [...webhookKeys.all(wsId), "deliveries", id] as const,
};

export function webhookListOptions(wsId: string) {
  const api = useApiClient();
  return queryOptions({
    queryKey: webhookKeys.list(wsId),
    queryFn: () => api.listWebhookEndpoints(),
  });
}

export function webhookDetailOptions(wsId: string, id: string) {
  const api = useApiClient();
  return queryOptions({
    queryKey: webhookKeys.detail(wsId, id),
    queryFn: () => api.getWebhookEndpoint(id),
    enabled: !!id,
  });
}

export function webhookDeliveriesOptions(wsId: string, endpointId: string) {
  const api = useApiClient();
  return queryOptions({
    queryKey: webhookKeys.deliveries(wsId, endpointId),
    queryFn: () => api.listWebhookDeliveries(endpointId),
    enabled: !!endpointId,
  });
}

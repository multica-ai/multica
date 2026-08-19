import { queryOptions, useMutation } from "@tanstack/react-query";
import { api } from "../api";
import type { WebPushSubscriptionRequest } from "../types";

export const webPushConfigOptions = () =>
  queryOptions({
    queryKey: ["web-push", "config"] as const,
    queryFn: () => api.getWebPushConfig(),
    staleTime: 5 * 60 * 1_000,
  });

export function useUpsertWebPushSubscription() {
  return useMutation({
    mutationKey: ["web-push", "subscription", "upsert"] as const,
    mutationFn: (subscription: WebPushSubscriptionRequest) =>
      api.upsertWebPushSubscription(subscription),
  });
}

export function useDeleteWebPushSubscription() {
  return useMutation({
    mutationKey: ["web-push", "subscription", "delete"] as const,
    mutationFn: (endpoint: string) => api.deleteWebPushSubscription(endpoint),
  });
}

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import { BUNDLED_PRICING, type ModelPricingRow } from "./pricing";

export const modelPricingKey = (wsId: string) =>
  ["model-pricing", wsId] as const;
export function modelPricingOptions(wsId: string) {
  return queryOptions({
    queryKey: modelPricingKey(wsId),
    queryFn: () => api.getModelPricing(wsId),
    enabled: Boolean(wsId),
    staleTime: 60_000,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
    retry: 1,
  });
}
export function useModelPricing(wsId: string) {
  const query = useQuery(modelPricingOptions(wsId));
  return { ...query, pricing: query.data ?? BUNDLED_PRICING };
}
export function useSaveModelPricing(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      revision: number;
      overrides: Record<string, ModelPricingRow>;
    }) => api.saveModelPricing(wsId, input),
    onSuccess: (data) => {
      client.setQueryData(modelPricingKey(wsId), data);
    },
    onError: () => client.invalidateQueries({ queryKey: modelPricingKey(wsId) }),
  });
}
export function useRefreshModelPricing(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => api.refreshModelPricing(wsId),
    onSuccess: (data) => {
      client.setQueryData(modelPricingKey(wsId), data);
    },
  });
}

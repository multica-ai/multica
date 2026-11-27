import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const trajectoryCapabilityKeys = {
  detail: (wsId: string) => ["trajectory-capability", wsId] as const,
};

export function trajectoryCapabilityOptions(wsId: string) {
  return queryOptions({
    queryKey: trajectoryCapabilityKeys.detail(wsId),
    queryFn: ({ signal }) => api.getTrajectoryCapability(wsId, signal),
    staleTime: 60_000,
    retry: false,
  });
}

import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

// Model Registry (FIR-2698) — TanStack Query options for the governance
// surface (live document, version history, change requests). The registry is
// a deployment-wide singleton, so keys carry no workspace/entity id — mirrors
// cerebro-agent-context/core/queries.ts.
export const modelRegistryKeys = {
  all: () => ["cerebro", "model-registry"] as const,
  live: () => [...modelRegistryKeys.all(), "live"] as const,
  versions: () => [...modelRegistryKeys.all(), "versions"] as const,
  changeRequests: () => [...modelRegistryKeys.all(), "change-requests"] as const,
};

export function modelRegistryOptions() {
  return queryOptions({
    queryKey: modelRegistryKeys.live(),
    queryFn: () => api.getModelRegistry(),
    staleTime: 30 * 1000,
  });
}

export function modelRegistryVersionsOptions() {
  return queryOptions({
    queryKey: modelRegistryKeys.versions(),
    queryFn: () => api.listModelRegistryVersions(),
    staleTime: 60 * 1000,
  });
}

export function modelRegistryChangeRequestsOptions() {
  return queryOptions({
    queryKey: modelRegistryKeys.changeRequests(),
    queryFn: () => api.listModelRegistryChangeRequests(),
    staleTime: 15 * 1000,
  });
}

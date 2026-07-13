import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { normalizeAgentObservability } from "./observability";

// Agent Office (FIR-1775) — TanStack Query options for an agent's context
// version history and change requests. Mirrors cerebro-skill-ownership/core.
export const agentContextKeys = {
  all: (agentId: string) => ["cerebro", "agent-context", agentId] as const,
  versions: (agentId: string) =>
    [...agentContextKeys.all(agentId), "versions"] as const,
  changeRequests: (agentId: string) =>
    [...agentContextKeys.all(agentId), "change-requests"] as const,
  observability: (agentId: string) =>
    [...agentContextKeys.all(agentId), "observability"] as const,
  pendingAll: () => ["cerebro", "agent-context", "pending"] as const,
};

export function agentContextVersionsOptions(agentId: string) {
  return queryOptions({
    queryKey: agentContextKeys.versions(agentId),
    queryFn: () => api.listAgentContextVersions(agentId),
    enabled: !!agentId,
    staleTime: 60 * 1000,
  });
}

export function agentContextChangeRequestsOptions(agentId: string) {
  return queryOptions({
    queryKey: agentContextKeys.changeRequests(agentId),
    queryFn: () => api.listAgentContextChangeRequests(agentId),
    enabled: !!agentId,
    staleTime: 30 * 1000,
  });
}

// FIR-1775 Phase 4 — the observability overview. Normalized defensively so a
// drifted server body degrades to zeros instead of white-screening.
export function agentContextObservabilityOptions(agentId: string) {
  return queryOptions({
    queryKey: agentContextKeys.observability(agentId),
    queryFn: async () =>
      normalizeAgentObservability(
        await api.getAgentContextObservability(agentId),
      ),
    enabled: !!agentId,
    staleTime: 30 * 1000,
  });
}

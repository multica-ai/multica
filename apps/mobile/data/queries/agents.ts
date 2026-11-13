import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const agentListOptions = (wsId: string | null, opts?: { includeArchived?: boolean }) =>
  queryOptions({
    queryKey: opts?.includeArchived
      ? ["agents", wsId, { includeArchived: true }] as const
      : ["agents", wsId] as const,
    queryFn: ({ signal }) => api.listAgents({ signal, includeArchived: opts?.includeArchived }),
    enabled: !!wsId,
    // Mirrors Web/Desktop: projected unstable ages offline without an event,
    // while offline recovery is covered by lifecycle events and reconnect.
    refetchInterval: (query) =>
      query.state.data?.some(
        (agent) =>
          !agent.archived_at &&
          (agent.runtime_availability === "online" ||
            agent.runtime_availability === "unstable"),
      )
        ? 30_000
        : false,
  });

import { queryOptions } from "@tanstack/react-query";
import { fetchAgentPasses } from "./api";

export const agentPassesKeys = {
  all: (wsId: string) => ["cerebro", "agent-passes", wsId] as const,
  list: (wsId: string) => [...agentPassesKeys.all(wsId), "list"] as const,
};

export function agentPassesListOptions(wsId: string) {
  return queryOptions({
    queryKey: agentPassesKeys.list(wsId),
    queryFn: fetchAgentPasses,
    enabled: !!wsId,
    staleTime: 15 * 1000,
    refetchOnWindowFocus: true,
    placeholderData: (prev) => prev,
  });
}

import { queryOptions } from "@tanstack/react-query";
import type { RuntimeBriefResponse } from "@multica/core/types";
import { api } from "@multica/core/api/client";

export function squadLeaderRuntimeOptions(wsId: string, squadId: string) {
  return queryOptions<RuntimeBriefResponse[]>({
    queryKey: ["squads", squadId, "leader-compatible-runtimes"] as const,
    queryFn: () => api.listSquadLeaderCompatibleRuntimes(squadId),
    enabled: !!wsId && !!squadId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

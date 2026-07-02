import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const teamKeys = {
  all: (wsId: string) => ["teams", wsId] as const,
  list: (wsId: string) => [...teamKeys.all(wsId), "list"] as const,
};

export function teamListOptions(wsId: string) {
  return queryOptions({
    queryKey: teamKeys.list(wsId),
    queryFn: () => api.listTeams(),
    select: (data) => data.teams,
  });
}

export function activeTeamListOptions(wsId: string) {
  // Shares teamListOptions' query key (and fetch/cache) — the active/archived
  // distinction is a per-observer `select`, not a separate cache entry, so
  // mutation cache patches on the base key are reflected here too.
  return queryOptions({
    queryKey: teamKeys.list(wsId),
    queryFn: () => api.listTeams(),
    select: (data) => data.teams.filter((team) => !team.archived_at),
  });
}

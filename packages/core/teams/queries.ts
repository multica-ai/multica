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
  return queryOptions({
    queryKey: [...teamKeys.list(wsId), "active"] as const,
    queryFn: () => api.listTeams(),
    select: (data) => data.teams.filter((team) => !team.archived_at),
  });
}

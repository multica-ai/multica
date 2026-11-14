import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";
import type { Squad, SquadMember } from "../types";

// Every key sits under `workspaceKeys.squads`, so the realtime sync's broad
// squads invalidation refreshes them too.
export const squadKeys = {
  detail: (wsId: string, squadId: string) => [...workspaceKeys.squads(wsId), squadId] as const,
  members: (wsId: string, squadId: string) => [...workspaceKeys.squads(wsId), squadId, "members"] as const,
  recentIssues: (wsId: string, squadId: string, limit: number) =>
    [...workspaceKeys.squads(wsId), squadId, "recent-issues", limit] as const,
};

export function squadDetailOptions(wsId: string, squadId: string) {
  return queryOptions<Squad>({
    queryKey: squadKeys.detail(wsId, squadId),
    queryFn: () => api.getSquad(squadId),
    enabled: !!wsId && !!squadId,
  });
}

export function squadMembersOptions(wsId: string, squadId: string) {
  return queryOptions<SquadMember[]>({
    queryKey: squadKeys.members(wsId, squadId),
    queryFn: () => api.listSquadMembers(squadId),
    enabled: !!wsId && !!squadId,
  });
}

/** The issues assigned to a squad, most recently updated first. */
export function squadRecentIssuesOptions(wsId: string, squadId: string, limit: number) {
  return queryOptions({
    queryKey: squadKeys.recentIssues(wsId, squadId, limit),
    queryFn: () =>
      api.listIssues({
        assignee_filters: [{ type: "squad", id: squadId }],
        sort_by: "updated_at",
        sort_direction: "desc",
        limit,
      }),
    enabled: !!wsId && !!squadId,
  });
}

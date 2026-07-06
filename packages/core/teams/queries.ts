import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Team } from "../types";

// Canonical presentation order for team lists shown to a user: joined teams
// first in the personal drag order (mirroring the sidebar's Teams section),
// then non-joined teams alphabetically. Pickers and filters share this so
// every team list a user sees is ordered the same way.
export function sortTeamsForDisplay(teams: Team[]): Team[] {
  const mine = teams
    .filter((team) => team.is_member)
    .sort((a, b) => a.sort_order - b.sort_order);
  const others = teams
    .filter((team) => !team.is_member)
    .sort((a, b) => a.name.localeCompare(b.name));
  return [...mine, ...others];
}

export const teamKeys = {
  all: (wsId: string) => ["teams", wsId] as const,
  list: (wsId: string) => [...teamKeys.all(wsId), "list"] as const,
  members: (wsId: string, teamId: string) =>
    [...teamKeys.all(wsId), "members", teamId] as const,
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
  // mutation cache patches on the base key are reflected here too. Sorted
  // with sortTeamsForDisplay so pickers match the sidebar's personal order.
  return queryOptions({
    queryKey: teamKeys.list(wsId),
    queryFn: () => api.listTeams(),
    select: (data) =>
      sortTeamsForDisplay(data.teams.filter((team) => !team.archived_at)),
  });
}

export function teamMembersOptions(wsId: string, teamId: string) {
  return queryOptions({
    queryKey: teamKeys.members(wsId, teamId),
    queryFn: () => api.listTeamMembers(teamId),
    select: (data) => data.members,
  });
}

export function myTeamListOptions(wsId: string) {
  // The sidebar's Teams section: only teams the user joined, in their
  // personal order. Same cache entry as teamListOptions (per-observer
  // select), so reorder patches on the base key reflect here instantly.
  return queryOptions({
    queryKey: teamKeys.list(wsId),
    queryFn: () => api.listTeams(),
    select: (data) =>
      data.teams
        .filter((team) => team.is_member && !team.archived_at)
        .sort((a, b) => a.sort_order - b.sort_order),
  });
}

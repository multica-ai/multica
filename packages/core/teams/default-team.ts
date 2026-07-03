import type { Team } from "../types";

/**
 * The team a new issue defaults into when no other context (route, parent,
 * single-team project) applies: the first team in the user's personal order.
 * Falls back to the workspace default team for users whose membership rows
 * predate the membership rollout, then to any active team.
 */
export function creationDefaultTeamId(teams: Team[]): string | undefined {
  const active = teams.filter((team) => !team.archived_at);
  const mine = active
    .filter((team) => team.is_member)
    .sort((a, b) => a.sort_order - b.sort_order);
  return (mine[0] ?? active.find((team) => team.is_default) ?? active[0])?.id;
}

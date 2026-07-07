import type { Space } from "../types";

/**
 * The space a new issue defaults into when no other context (route, parent,
 * single-space project) applies: the first space in the user's personal order.
 * Falls back to the workspace default space for users whose membership rows
 * predate the membership rollout, then to any active space.
 */
export function creationDefaultSpaceId(spaces: Space[]): string | undefined {
  const active = spaces.filter((space) => !space.archived_at);
  const mine = active
    .filter((space) => space.is_member)
    .sort((a, b) => a.sort_order - b.sort_order);
  return (mine[0] ?? active.find((space) => space.is_default) ?? active[0])?.id;
}

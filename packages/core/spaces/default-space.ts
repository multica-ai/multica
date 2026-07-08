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

/**
 * Single fallback chain for "which space does this new issue/project/autopilot
 * belong to", shared by every creation surface. Callers pass whichever
 * signals apply to them (e.g. only issue creation has a parent to inherit
 * from) and leave the rest undefined.
 *
 * Priority: structural inheritance (parent issue's space) > single-space
 * project inference > the space the user last created something in >
 * personal static default (first space in my sort order).
 *
 * Explicit picks and view/route context (e.g. a space's own Issues page)
 * are NOT part of this chain — callers seed their own local `spaceId` state
 * from those directly and only fall through to this resolver while that
 * state is still unset.
 */
export function resolveCreationSpaceId(
  spaces: Space[],
  ctx: { parentSpaceId?: string; projectSpaceId?: string; lastSpaceId?: string | null },
): string | undefined {
  return (
    ctx.parentSpaceId ??
    ctx.projectSpaceId ??
    ctx.lastSpaceId ??
    creationDefaultSpaceId(spaces)
  );
}

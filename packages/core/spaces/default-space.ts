import type { Space } from "../types";

/**
 * The stable workspace Default Space used when no structural or route context
 * applies. The earliest active Space is a compatibility fallback for clients
 * talking to a server that predates the explicit is_default field.
 */
export function creationDefaultSpaceId(spaces: Space[]): string | undefined {
  const active = spaces.filter((space) => !space.archived_at);
  const configured = active.find((space) => space.is_default);
  const earliest = [...active].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  )[0];
  return (configured ?? earliest)?.id;
}

/**
 * Single fallback chain for "which space does this new issue/project/autopilot
 * belong to", shared by every creation surface. Callers pass whichever
 * signals apply to them (e.g. only issue creation has a parent to inherit
 * from) and leave the rest undefined.
 *
 * Priority: structural inheritance (parent issue's space) > single-space
 * project inference > workspace Default Space.
 *
 * Explicit picks and view/route context (e.g. a space's own Issues page)
 * are NOT part of this chain — callers seed their own local `spaceId` state
 * from those directly and only fall through to this resolver while that
 * state is still unset.
 */
export function resolveCreationSpaceId(
  spaces: Space[],
  ctx: { parentSpaceId?: string; projectSpaceId?: string },
): string | undefined {
  return (
    ctx.parentSpaceId ??
    ctx.projectSpaceId ??
    creationDefaultSpaceId(spaces)
  );
}

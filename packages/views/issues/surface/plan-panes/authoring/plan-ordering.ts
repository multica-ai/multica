/**
 * Position arithmetic for plan authoring. Pure on purpose: the reorder
 * contract is strict enough that it is worth testing without a DOM.
 *
 * `ReorderPhases` / `ReorderParts` run `validateExactOrder`
 * (server/internal/projectplan/service.go), which rejects any list that is not
 * a permutation of every current sibling — so a move must always send the
 * complete ordered set, never just the two ids that swapped.
 */

/**
 * The position a newly appended sibling should take: one past the highest in
 * use. Derived from the real `position` values, not from `length`, because
 * `position` is a unique key per parent
 * (`project_plan_phase_plan_position_key`) and deletions leave gaps — a
 * length-based guess would collide with a surviving sibling and come back as a
 * position conflict.
 */
export function nextPosition(siblings: { position: number }[]): number {
  return siblings.reduce((max, sibling) => Math.max(max, sibling.position + 1), 0);
}

/**
 * The full ordered id list after moving one item one slot up or down.
 *
 * Returns null when the move is a no-op (item already at the boundary, or not
 * in the list) so callers can skip the request rather than send an unchanged
 * permutation.
 */
export function movedOrder(
  siblings: { id: string }[],
  id: string,
  direction: "up" | "down",
): string[] | null {
  const ids = siblings.map((sibling) => sibling.id);
  const from = ids.indexOf(id);
  if (from === -1) return null;
  const to = direction === "up" ? from - 1 : from + 1;
  if (to < 0 || to >= ids.length) return null;
  const reordered = [...ids];
  const [moved] = reordered.splice(from, 1);
  reordered.splice(to, 0, moved!);
  return reordered;
}

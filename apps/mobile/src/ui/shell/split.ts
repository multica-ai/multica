import type { Fold } from "../../platform/fold-layout";

/** Narrower than this, a list and its detail take turns on the screen. */
const WIDE = 600;

/**
 * How wide the list column is beside its detail, or null when there is room for one column only.
 * A fold that runs top to bottom, like iPhone Duo's hinge held sideways, places the divider on it.
 */
export function listColumnWidth(width: number, folds: readonly Fold[]): number | null {
  if (width < WIDE) return null;
  const hinge = folds.find((fold) => fold.height > fold.width && fold.x > 0 && fold.x + fold.width < width);
  if (hinge) return Math.round(hinge.x + hinge.width / 2);
  return Math.round(Math.min(Math.max(width * 0.43, 280), 420));
}

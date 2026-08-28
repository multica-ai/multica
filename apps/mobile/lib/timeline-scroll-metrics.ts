/**
 * Physical scroll-end geometry helpers for the timeline (RUYI-28).
 *
 * Extracted as pure functions so the "independent bottom chip" rules stay
 * testable: the chip depends ONLY on physical distance to the FlashList's
 * content end — never on `newCount`, the unread divider, or last-viewed
 * state (approved scope keeps those systems untouched).
 *
 * The existing AT_BOTTOM_SLACK_PX (80px, "user is about to see the bottom")
 * lives inside timeline-list.tsx and drives the new-message chip counter;
 * these helpers use a separate, tighter 48px band for a dedicated
 * "you are far from the end" affordance. Two different questions, two
 * different thresholds.
 */
export const BOTTOM_CHIP_MIN_GAP_PX = 48;

/**
 * Pixel distance from the scroll viewport's bottom edge to the physical
 * end of the content. Clamped at 0 — overscroll (bounce / negative
 * rubber-band values) must not report a negative "distance" that callers
 * would compare against a positive threshold incorrectly.
 *
 * When contentHeight < viewport the user can never scroll away from the
 * end; the clamp yields 0 via the max().
 */
export function distToPhysicalEnd(
  contentHeight: number,
  offsetY: number,
  viewportHeight: number,
): number {
  return Math.max(0, contentHeight - (offsetY + viewportHeight));
}

/**
 * The standalone "to bottom" chip is visible only when the user is
 * strictly farther than BOTTOM_CHIP_MIN_GAP_PX from the physical end.
 * Inside the band the list is effectively at the end — the chip would
 * cover content the user is already reading.
 */
export function shouldShowBottomChip(distFromEnd: number): boolean {
  return distFromEnd > BOTTOM_CHIP_MIN_GAP_PX;
}

/**
 * Last-known scroll geometry, saved by the timeline so the bottom chip can
 * be recomputed OUTSIDE scroll events — `onScroll` only fires once the user
 * actually drags, so a chip driven solely from it stays stale (or absent)
 * on mount, after data grows, and after the viewport resizes.
 */
export interface ScrollGeometry {
  contentHeight: number;
  offsetY: number;
  viewportHeight: number;
}

/**
 * Unified visibility decision over a geometry snapshot. Pure — the same
 * function answers both the scroll-event path and the mount /
 * onContentSizeChange / onLayout path, so the chip can never disagree with
 * itself between the two entry points.
 *
 * Zeroed geometry (nothing laid out yet) yields "at end": showing the chip
 * before the first layout would flash it over an empty list.
 */
export function computeBottomChipVisible(geo: ScrollGeometry): boolean {
  if (geo.viewportHeight <= 0) return false;
  return shouldShowBottomChip(
    distToPhysicalEnd(geo.contentHeight, geo.offsetY, geo.viewportHeight),
  );
}

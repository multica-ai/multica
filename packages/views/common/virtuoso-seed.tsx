"use client";

import { Fragment, type Key, type ReactNode } from "react";

/**
 * Upper bound on rows rendered in the first-paint seed ({@link VirtuosoSeed})
 * and passed as `initialItemCount` to the virtualized issue/inbox lists.
 *
 * One screen's worth of rows is enough to fill the viewport on a route-return
 * remount; the real Virtuoso trims to its measured window on the next frame.
 * Capped so a large list never pays a full synchronous mount — the crash the
 * un-capped pre-virtualization path hit on real Desktop (MUL-4750).
 */
export const VIRTUOSO_SEED_COUNT = 30;

/**
 * Non-virtualized fallback for the frame(s) before a Virtuoso's
 * `customScrollParent` is ready.
 *
 * The issue/inbox lists hand Virtuoso the scroll element through a callback
 * ref that lands in state, so the first render after a remount has
 * `scrollParent === null` and Virtuoso cannot mount yet. Rendering nothing
 * there paints an empty card area (group/column headers present, rows blank)
 * until the ref settles and measurement completes — the flash that got the
 * MUL-4474 virtualization reverted. Seeding a bounded slice of the real rows
 * keeps that first frame populated; once the parent is set the list switches
 * to `<Virtuoso>` with a matching `initialItemCount`, so the handoff is
 * visually continuous.
 *
 * Reuses the caller's own `itemContent`/`computeItemKey`, so a seeded row is
 * byte-identical to its virtualized counterpart — there is no second render
 * path to drift.
 */
export function VirtuosoSeed<T>({
  data,
  itemContent,
  computeItemKey,
  count = VIRTUOSO_SEED_COUNT,
}: {
  data: T[];
  itemContent: (index: number, item: T) => ReactNode;
  computeItemKey: (index: number, item: T) => Key;
  count?: number;
}) {
  return (
    <>
      {data.slice(0, count).map((item, index) => (
        <Fragment key={computeItemKey(index, item)}>
          {itemContent(index, item)}
        </Fragment>
      ))}
    </>
  );
}

import type { TimelineEntry } from "@multica/core/types";

/**
 * Pure comparator: `created_at` ASC with `id` tie-break. Touches no arrays.
 *
 * Use this directly when you need to sort a self-owned array (e.g. an
 * internal accumulator). For display-layer sorting where the input may be a
 * shared React Query cache reference, use {@link sortTimelineEntries}, which
 * copies before sorting.
 */
export function compareTimelineEntriesAsc(
  a: TimelineEntry,
  b: TimelineEntry,
): number {
  if (a.created_at !== b.created_at) {
    return a.created_at < b.created_at ? -1 : 1;
  }
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/**
 * Stable-ascending sort for flat TimelineEntry[] caches.
 *
 * All writers that append to an issue timeline cache MUST pass through
 * this helper so the display order stays `created_at` ASC (id tie-breaker)
 * even when WebSocket events and mutation onSuccess callbacks arrive
 * out of chronological order.
 *
 * This sorts IN PLACE — callers MUST pass a self-owned array (a fresh
 * `[...old, entry]` spread, an internal accumulator, etc.). Never feed a
 * React Query cache reference directly here; use the pure
 * {@link sortTimelineEntries} for display-layer ordering instead.
 *
 * Observers that mutate in place (map / filter by id) don't need this —
 * they preserve the existing relative order.
 */
export function sortTimelineEntriesAsc(entries: TimelineEntry[]): TimelineEntry[] {
  entries.sort(compareTimelineEntriesAsc);
  return entries;
}

export type TimelineSortDirection = "oldest" | "newest";

/**
 * Display-layer sort: returns a NEW array sorted in the requested direction
 * without mutating the input. Safe to call with a React Query cache array —
 * the copy happens before any sorting or reversal, so the cache stays in
 * canonical ASC order.
 */
export function sortTimelineEntries(
  entries: readonly TimelineEntry[],
  direction: TimelineSortDirection,
): TimelineEntry[] {
  const copy = entries.slice();
  copy.sort(compareTimelineEntriesAsc);
  if (direction === "newest") copy.reverse();
  return copy;
}

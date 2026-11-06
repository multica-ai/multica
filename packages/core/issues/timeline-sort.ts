import type { TimelineDirection, TimelineEntry } from "@multica/core/types";

/**
 * Stable-ascending sort for flat TimelineEntry[] caches.
 *
 * Used for chronological thread replies and ascending caches. Live events
 * use insertTimelineEntry with the cache's requested server direction so
 * out-of-order delivery preserves both ascending and descending timelines.
 *
 * Observers that mutate in place (map / filter by id) don't need this —
 * they preserve the existing relative order.
 */
export function sortTimelineEntriesAsc(entries: TimelineEntry[]): TimelineEntry[] {
  entries.sort((a, b) => {
    if (a.created_at !== b.created_at) {
      return a.created_at < b.created_at ? -1 : 1;
    }
    return a.id < b.id ? -1 : 1;
  });
  return entries;
}

/** Insert a live event without re-sorting the server-ordered timeline. */
export function insertTimelineEntry(
  entries: readonly TimelineEntry[],
  entry: TimelineEntry,
  direction: TimelineDirection,
): TimelineEntry[] {
  const index = entries.findIndex((existing) => {
    const comparison = entry.created_at.localeCompare(existing.created_at) || entry.id.localeCompare(existing.id);
    return direction === "asc" ? comparison < 0 : comparison > 0;
  });
  const result = [...entries];
  result.splice(index < 0 ? result.length : index, 0, entry);
  return result;
}

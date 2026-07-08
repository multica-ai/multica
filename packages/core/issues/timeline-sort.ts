import type { TimelineEntry } from "@multica/core/types";

/**
 * Stable-ascending sort for flat TimelineEntry[] caches.
 *
 * All writers that append to an issue timeline cache MUST pass through
 * this helper so the display order stays `created_at` ASC (id tie-breaker)
 * even when WebSocket events and mutation onSuccess callbacks arrive
 * out of chronological order.
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

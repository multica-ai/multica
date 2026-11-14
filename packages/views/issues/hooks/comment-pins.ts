import type { TimelineEntry } from "@multica/core/types";

/** Mirrors the server's per-issue limit (MaxPinnedCommentsPerIssue). */
export const MAX_PINNED_COMMENTS = 5;

/**
 * `topInset` is the height the sticky pinned bar covers at the viewport top;
 * `bottomInset` the height a sticky bottom overlay (the comment composer)
 * covers at the bottom. Content under either is not readable.
 */
export function pinBodyVisible(body: Pick<DOMRect, "top" | "bottom" | "height">, viewport: Pick<DOMRect, "top" | "bottom">, bottomInset = 0, topInset = 0) {
  return body.height > 0 && Math.min(body.bottom, viewport.bottom - bottomInset) - Math.max(body.top, viewport.top + topInset) >= Math.min(24, body.height);
}
export function sameIds(a: ReadonlySet<string>, b: ReadonlySet<string>) {
  return a.size === b.size && [...a].every(id => b.has(id));
}
/** Every live pin, in the order it was pinned, so the stacked bar never reorders while reading. */
export function orderedPins(entries: readonly TimelineEntry[]) {
  return entries.filter(e => e.type === "comment" && e.pinned_at && !e.deleted_at)
    .sort((a, b) => (a.pinned_at ?? "").localeCompare(b.pinned_at ?? "") || a.id.localeCompare(b.id));
}

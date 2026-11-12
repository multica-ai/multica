import type { TimelineEntry } from "@multica/core/types";

// Reserve the collapsed overlay's space even while hidden, avoiding feedback
// between hiding the bar and declaring the newly uncovered body visible.
export const PIN_OCCLUSION = 64;
/**
 * `bottomInset` is the height a sticky bottom overlay (the comment composer)
 * covers at the viewport's bottom edge; content under it is not readable.
 */
export function pinBodyVisible(body: Pick<DOMRect, "top" | "bottom" | "height">, viewport: Pick<DOMRect, "top" | "bottom">, bottomInset = 0) {
  return body.height > 0 && Math.min(body.bottom, viewport.bottom - bottomInset) - Math.max(body.top, viewport.top + PIN_OCCLUSION) >= Math.min(24, body.height);
}
export function sameIds(a: ReadonlySet<string>, b: ReadonlySet<string>) {
  return a.size === b.size && [...a].every(id => b.has(id));
}
export function offscreenPins(entries: readonly TimelineEntry[], visible: ReadonlySet<string>) {
  return entries.filter(e => e.type === "comment" && e.pinned_at && !e.deleted_at && !visible.has(e.id))
    .sort((a, b) => (b.pinned_at ?? "").localeCompare(a.pinned_at ?? "") || a.id.localeCompare(b.id));
}

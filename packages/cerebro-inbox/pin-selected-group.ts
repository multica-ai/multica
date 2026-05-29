// FIR-2474 — keep the currently-open inbox row in the same GROUP until closed.
//
// `sortInboxEntriesPinned` (see ./pin-selected) already freezes the open row's
// sort *time* so it keeps its position in the flat, time-sorted list. But the
// inbox can also be GROUPED ("Group by → Action / Project / Agent / Type" — and
// Action is the default for new users). When grouping is on, every row is
// bucketed live on each render from signals that change with activity (run
// state, mentions, read/unread). So a new comment or agent reply on the open
// row recomputes its bucket and the row jumps to a different group — even though
// its time is frozen. The user only sees it "stay" when the incoming activity
// happens not to change its bucket ("kun liggende i den samme sortering").
//
// This helper freezes the open row's bucket assignment per group-mode the
// moment it is opened, so incoming activity can no longer move it to another
// group. Every other row buckets live. The pin is released when the selection
// clears (message closed) or when the user deliberately changes the grouping
// (a user action, not incoming activity), at which point the row re-buckets
// naturally on the next pass.

export interface InboxBucket {
  key: string;
  label: string;
  isFallback: boolean;
  order?: number;
}

export interface PinnedGroup {
  /** The selected row's key (issue id, channel id or chat-session id). */
  key: string;
  /** Grouping signature this pin was captured under (primary+secondary modes). */
  sig: string;
  /** The selected row's bucket, frozen per group-mode at the moment it opened. */
  byMode: Record<string, InboxBucket>;
}

/**
 * Return a bucketize-like function that freezes the selected row's group
 * placement while it is open.
 *
 * `pinnedRef` is a mutable ref owned by the caller; this function reads and
 * updates `pinnedRef.current`:
 *  - no selection → the pin is cleared and every row buckets live;
 *  - a new selection, or a changed grouping signature → the selected row's
 *    current bucket is captured for each active mode;
 *  - an unchanged selection under the same grouping → the captured buckets are
 *    kept, so incoming activity no longer re-buckets the open row.
 *
 * If the selected row isn't present in `entries` yet (e.g. a deep-linked item
 * not in this list), the capture waits for a later pass rather than freezing a
 * wrong bucket — mirroring `sortInboxEntriesPinned`.
 *
 * `keyOf` maps an entry to the same key space as `selectedKey`. `modes` are the
 * active group-by modes (primary + secondary); `"none"` is ignored.
 */
export function pinnedBucketizer<E, M extends string>(
  bucketize: (mode: M, entry: E) => InboxBucket,
  keyOf: (entry: E) => string,
  selectedKey: string,
  selectedEntry: E | undefined,
  modes: M[],
  pinnedRef: { current: PinnedGroup | null },
): (mode: M, entry: E) => InboxBucket {
  const active = modes.filter((m) => m && (m as string) !== "none");
  const sig = [...active].sort().join(",");

  if (!selectedKey) {
    pinnedRef.current = null;
  } else if (pinnedRef.current?.key !== selectedKey || pinnedRef.current?.sig !== sig) {
    if (selectedEntry) {
      const byMode: Record<string, InboxBucket> = {};
      for (const mode of active) byMode[mode] = bucketize(mode, selectedEntry);
      pinnedRef.current = { key: selectedKey, sig, byMode };
    }
  }

  const pin = pinnedRef.current;
  return (mode, entry) => {
    if (pin && keyOf(entry) === pin.key && pin.byMode[mode]) return pin.byMode[mode];
    return bucketize(mode, entry);
  };
}

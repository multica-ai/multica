// FIR-2474 — keep the currently-open inbox row anchored in place.
//
// The inbox list sorts every row by its latest-activity time (newest first).
// While the user has a row open, fresh activity for that same row (a new
// comment, an agent reply, a new notification) bumps its sort time and the row
// jumps — usually straight to the top — under the reader's cursor. That is
// disorienting and loses the user's place.
//
// This helper freezes the open row's sort time to the value it had the moment
// it was opened, so it stays exactly where it was. The row only re-sorts to its
// natural position once the message is closed (selection cleared). Everything
// else keeps sorting live.

export interface PinnedSelection {
  /** The selected row's key (issue id, channel id or chat-session id). */
  key: string;
  /** The row's sort time captured at the moment it was opened. */
  time: number;
}

/**
 * Return a sorted copy of `entries` (newest first) with the selected row pinned
 * to the sort time it had when it was first opened.
 *
 * `pinnedRef` is a mutable ref owned by the caller; this function reads and
 * updates `pinnedRef.current`:
 *  - no selection → the pin is cleared and rows sort fully live;
 *  - a new selection → the selected row's current time is captured as the pin;
 *  - an unchanged selection → the captured time is kept, so incoming activity
 *    for the open row no longer reorders it.
 *
 * `keyOf` maps an entry to the same key space as `selectedKey`.
 */
export function sortInboxEntriesPinned<T extends { time: number }>(
  entries: T[],
  keyOf: (entry: T) => string,
  selectedKey: string,
  pinnedRef: { current: PinnedSelection | null },
): T[] {
  if (!selectedKey) {
    pinnedRef.current = null;
  } else if (pinnedRef.current?.key !== selectedKey) {
    const selected = entries.find((entry) => keyOf(entry) === selectedKey);
    // If the row isn't present yet (e.g. a deep-linked item not in this list),
    // wait for a later pass rather than pinning a wrong value.
    if (selected) pinnedRef.current = { key: selectedKey, time: selected.time };
  }

  const pinned = pinnedRef.current;
  const effectiveTime = (entry: T): number =>
    pinned && keyOf(entry) === pinned.key ? pinned.time : entry.time;

  return [...entries].sort((a, b) => effectiveTime(b) - effectiveTime(a));
}

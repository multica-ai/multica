// FIR-1576 — a session-persistent set of every channel/DM id the inbox has
// ever seen in the channel list.
//
// Why this exists: an inbox row for a DM/channel is a *folded* view — the
// notification for the channel is swallowed into the channel row, but ONLY
// while the channel is present in `channelMap` (the channel-list cache). When
// the user archives a DM, the per-user channel archive removes the channel
// from that cache optimistically; and when a new message later re-surfaces the
// DM, the notification (inbox:new) lands in the inbox cache *before* the
// channel returns via a separate channel-list refetch. In that gap the inbox
// no longer recognises the id as a channel, so the notification "unfolds" into
// a bare row that (a) makes the archived DM reappear and (b) routes a click
// through the generic issue path, which redirects DM/channel issues out of the
// inbox to /channels/<id> (FIR-2452).
//
// `InboxItem` carries no issue-kind, so `channelMap` is the only signal that an
// id is a channel — and it is exactly the cache the archive empties. This set
// remembers every id that was ever a channel for the lifetime of the mounted
// inbox, so the fold rule and the detail/redirect guards keep treating it as a
// channel across the re-surface window. The set only grows (bounded by the
// number of distinct channels seen); ids are never removed, because "this id
// is a channel" does not stop being true when the row is transiently absent.
import { useEffect, useRef, useState } from "react";

/**
 * Adds every channel id in `channels` to `known`, mutating it in place.
 * Returns true when at least one new id was added (so a caller can decide
 * whether a re-render is needed). Pure aside from mutating the passed set.
 */
export function accumulateChannelIds(
  known: Set<string>,
  channels: ReadonlyArray<{ id: string }>,
): boolean {
  let added = false;
  for (const channel of channels) {
    if (!known.has(channel.id)) {
      known.add(channel.id);
      added = true;
    }
  }
  return added;
}

/**
 * Returns a session-persistent set of every channel/DM id seen in `channels`.
 * The set survives a channel briefly dropping out of the list (optimistic
 * archive, re-surface refetch lag), so the inbox keeps recognising the id as a
 * channel. A re-render is triggered the first time a new id is observed so
 * consumers re-evaluate folding/selection against the updated set.
 */
export function useKnownChannelIds(
  channels: ReadonlyArray<{ id: string }>,
): ReadonlySet<string> {
  const knownRef = useRef<Set<string>>(new Set());
  const [, bump] = useState(0);
  useEffect(() => {
    if (accumulateChannelIds(knownRef.current, channels)) {
      bump((n) => n + 1);
    }
  }, [channels]);
  return knownRef.current;
}

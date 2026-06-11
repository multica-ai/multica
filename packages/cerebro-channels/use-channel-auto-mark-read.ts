// TECH-3352 — auto-mark a channel/DM read when the user opens it.
//
// Background: TECH-3300 added a focus guard (only mark read while the tab is
// visible AND the window is focused) so a message arriving in a *background*
// tab — while the conversation sits mounted in the inbox split view — does not
// silently clear its unread state. That guard was too broad: it also gated the
// *initial open*. When focus flickered at mount time (an OS-notification click,
// a window switch), the open-time read was skipped and never retried, so the
// inbox row stayed styled as unread even though the user had read the thread.
//
// Fix: split the two cases.
//   - First time we see a channel = the user opened it = explicit read intent.
//     Mark read immediately, regardless of focus.
//   - A later unread bump while the conversation is already open = a new
//     message. Keep the focus guard so a background-tab message stays unread
//     until the user actually looks.
import { useEffect, useRef } from "react";
import { useMarkChannelRead } from "@multica/core/channels";

/**
 * Pure decision: should we fire a mark-read right now? Extracted so the
 * (focus-dependent, ref-tracked) policy can be unit-tested without a DOM.
 *
 * - `freshOpen` forces a read regardless of focus — opening a conversation is
 *   explicit read intent (the TECH-3352 fix).
 * - Otherwise the focus guard applies: a new message arriving while the thread
 *   is already open is only cleared when the tab is visible AND focused
 *   (the TECH-3300 behaviour).
 * - `(id, count)` equality stops duplicate fires and API-failure loops.
 */
export function shouldMarkChannelRead(params: {
  freshOpen: boolean;
  visible: boolean;
  focused: boolean;
  last: { id: string; count: number } | null;
  channelId: string;
  count: number;
}): boolean {
  const { freshOpen, visible, focused, last, channelId, count } = params;
  if (!freshOpen && (!visible || !focused)) return false;
  if (last?.id === channelId && last.count === count) return false;
  return true;
}

export function useChannelAutoMarkRead(
  channelId: string,
  unreadCount: number | undefined,
) {
  const markChannelRead = useMarkChannelRead();
  // The channel id we've already treated as "opened". Recorded the first time
  // we see real unread data for it, so a later unread bump is no longer a fresh
  // open and falls under the focus guard.
  const openedRef = useRef<string | null>(null);
  // (id, count) we last marked read for — stops a repeated render with the same
  // unread count from re-firing, and stops an API failure from looping.
  const seenRef = useRef<{ id: string; count: number } | null>(null);

  useEffect(() => {
    if (!channelId) return;
    // Wait for real channel data — `undefined` means the detail query hasn't
    // resolved yet. `0` is real data (already read) and must record the open so
    // a subsequent message isn't mistaken for a fresh open.
    if (unreadCount === undefined) return;
    const freshOpen = openedRef.current !== channelId;
    if (freshOpen) openedRef.current = channelId;
    if (unreadCount === 0) return;

    const count = unreadCount;
    const mark = (force: boolean) => {
      const ok = shouldMarkChannelRead({
        freshOpen: force,
        visible: document.visibilityState === "visible",
        focused: document.hasFocus(),
        last: seenRef.current,
        channelId,
        count,
      });
      if (!ok) return;
      seenRef.current = { id: channelId, count };
      markChannelRead.mutate(channelId);
    };

    // Force on the first open of this channel (explicit intent); guard the rest.
    mark(freshOpen);
    if (freshOpen) return;

    const onActive = () => mark(false);
    window.addEventListener("focus", onActive);
    document.addEventListener("visibilitychange", onActive);
    return () => {
      window.removeEventListener("focus", onActive);
      document.removeEventListener("visibilitychange", onActive);
    };
  }, [channelId, unreadCount, markChannelRead]);
}

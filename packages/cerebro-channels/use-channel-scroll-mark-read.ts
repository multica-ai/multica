// FIR-2010 — mark a channel read when the user scrolls PAST its messages
// (reaches the bottom), instead of on open.
//
// Background: the smart-unread mode (cerebro_channel_unread_smart) treats a
// channel like Slack/Teams — a channel badge only goes red on an @mention, and
// simply opening a busy channel should NOT clear its unread state the way a DM
// does. Jesper's answer to "when is a chat read?" was explicit: a channel is
// read once you have scrolled past its messages, not the instant you click it.
//
// This hook is the channel-only counterpart to useChannelAutoMarkRead (which
// stays the open-time read path for DMs). It fires the same channel-level
// mark-read mutation, so notifications-routed rows are cleared too, but only
// once the user is actually at the bottom of the conversation.
import { useEffect, useRef } from "react";
import { useMarkChannelRead } from "@multica/core/channels";

/**
 * Pure decision: should we mark this channel read on scroll right now?
 * Extracted so the policy can be unit-tested without a DOM.
 *
 * - Only fires when the feature is `enabled` for a real channel id.
 * - Requires `hasUnread` (something to clear) AND `atBottom` (the user has
 *   scrolled past the messages).
 * - Requires the tab to be visible AND focused, so a background-tab scroll
 *   position does not silently clear unread (mirrors the TECH-3300 guard).
 * - `(channelId + signature)` equality stops duplicate fires and API-failure
 *   loops; a new message changes the signature so a later read still fires.
 */
export function shouldMarkChannelReadOnScroll(params: {
  enabled: boolean;
  channelId: string;
  hasUnread: boolean;
  atBottom: boolean;
  visible: boolean;
  focused: boolean;
  last: string | null;
  signature: string;
}): boolean {
  const { enabled, channelId, hasUnread, atBottom, visible, focused, last, signature } = params;
  if (!enabled || !channelId) return false;
  if (!hasUnread || !atBottom) return false;
  if (!visible || !focused) return false;
  if (last === `${channelId}:${signature}`) return false;
  return true;
}

export function useChannelScrollMarkRead(params: {
  channelId: string;
  /** True when the channel has any unread activity (mention or not). */
  hasUnread: boolean;
  /** True when the user is at the bottom of the message scroll container. */
  atBottom: boolean;
  /** Gate — only the smart-unread channel path mounts this behavior. */
  enabled: boolean;
  /** Changes when a new message lands, so a later read re-fires. */
  signature: string;
}) {
  const { channelId, hasUnread, atBottom, enabled, signature } = params;
  const markChannelRead = useMarkChannelRead();
  // The `channelId:signature` we last marked read for — stops a repeated render
  // at the same scroll position from re-firing, and stops an API-failure loop.
  const seenRef = useRef<string | null>(null);

  useEffect(() => {
    const ok = shouldMarkChannelReadOnScroll({
      enabled,
      channelId,
      hasUnread,
      atBottom,
      visible: typeof document !== "undefined" && document.visibilityState === "visible",
      focused: typeof document !== "undefined" && document.hasFocus(),
      last: seenRef.current,
      signature,
    });
    if (!ok) return;
    seenRef.current = `${channelId}:${signature}`;
    markChannelRead.mutate(channelId);
  }, [enabled, channelId, hasUnread, atBottom, signature, markChannelRead]);
}

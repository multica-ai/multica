// FIR-4649 — shared Chat unread predicates. The Chat rail, Chat badge and
// (when smart-unread is on) the inbox all need the same rule: a channel is
// unread when it has a red count OR any unread activity (bold-dot signal).
import type { Channel } from "@multica/core/types";

/** True when the Chat rail should treat this channel/DM as unread. */
export function channelHasChatUnread(channel: Channel): boolean {
  return (channel.unread_count ?? 0) > 0 || channel.has_unread_activity === true;
}

/**
 * Badge number for a Chat rail row. Mentions keep their real count; activity-
 * only (smart-unread non-mention) shows as 1 so the row still ranks unread and
 * the pill is visible. 0 means no badge.
 */
export function channelChatUnreadBadge(channel: Channel): number {
  if ((channel.unread_count ?? 0) > 0) return channel.unread_count;
  return channel.has_unread_activity === true ? 1 : 0;
}

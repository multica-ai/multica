// FIR-4350 / FIR-4649 — pure Chat unread counter (no React / query hooks).
import type { Channel, ChatSession, InboxItem } from "@multica/core/types";
import { channelHasChatUnread } from "./channel-chat-unread";
import { buildUnreadChannelThreads } from "./channel-thread-entries";
import {
  conversationKindOfChannel,
  showsInChat,
  type ChatPlacement,
} from "./chat-placement";

/**
 * Pure + exported so the counting rule is unit-tested without a query client.
 * A channel counts once when it has any unread, matching how the inbox badge
 * counts conversations rather than messages. Unread threads count separately
 * (FIR-1854 / FIR-4649) when the thread-split surface is on.
 */
export function countUnreadConversations(
  rosterChannels: Channel[],
  dmChannels: Channel[],
  sessions: ChatSession[],
  placement: ChatPlacement,
  rawInbox: InboxItem[] = [],
  threadSplitEnabled = false,
): number {
  // Named channels + groups: the rail's Channels group renders these from the
  // include-archived roster, so an inbox-archived one still shows in Chat and
  // still counts. (Groups map to the DM placement via conversationKindOfChannel.)
  const namedCount = rosterChannels.filter((c) => {
    if (c.kind !== "channel" && c.kind !== "group") return false;
    if (!showsInChat(placement, conversationKindOfChannel(c.kind))) return false;
    return channelHasChatUnread(c);
  }).length;
  // DMs: the rail's People section matches these from the NON-archived list, so
  // an inbox-archived DM (e.g. one snoozed as a reminder) is hidden from Chat.
  // It must not count here, or the badge promises an unread the user cannot find.
  const dmCount = showsInChat(placement, "dm")
    ? dmChannels.filter((c) => c.kind === "dm" && channelHasChatUnread(c)).length
    : 0;
  const sessionCount = showsInChat(placement, "agent_chat")
    ? sessions.filter((s) => s.status !== "archived" && s.has_unread === true).length
    : 0;

  let threadCount = 0;
  if (threadSplitEnabled && rawInbox.length > 0) {
    const channelMap = new Map<string, Channel>();
    for (const c of rosterChannels) channelMap.set(c.id, c);
    for (const c of dmChannels) channelMap.set(c.id, c);
    for (const t of buildUnreadChannelThreads(rawInbox, channelMap)) {
      if (!showsInChat(placement, conversationKindOfChannel(t.channel.kind))) continue;
      threadCount += 1;
    }
  }

  return namedCount + dmCount + sessionCount + threadCount;
}

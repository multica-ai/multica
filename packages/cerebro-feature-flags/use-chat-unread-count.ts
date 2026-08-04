// FIR-4350 — the Chat entry's badge: unread CONVERSATIONS only, and only the
// types placed in Chat. Counted client-side over the queries the rail and both
// inbox implementations already fetch, so it adds no request.
//
// Counted from Channel.unread_count and ChatSession.has_unread — the same
// signals the rail rows badge on — from the SAME lists the rail renders from:
// named channels/groups from the include-archived roster (they persist in
// Chat), DMs from the non-archived list (the rail's People section hides
// inbox-archived DMs). Counting DMs from the roster instead made the badge show
// an unread for a snoozed/archived DM the user could not find in Chat.
"use client";

import { useQuery } from "@tanstack/react-query";
import { channelListOptions, channelRosterListOptions } from "@multica/core/channels";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import type { Channel, ChatSession } from "@multica/core/types";
import {
  conversationKindOfChannel,
  showsInChat,
  type ChatPlacement,
} from "./chat-placement";
import { useChatPlacement } from "./use-chat-placement";

/**
 * Pure + exported so the counting rule is unit-tested without a query client.
 * A channel counts once when it has any unread, matching how the inbox badge
 * counts conversations rather than messages.
 */
export function countUnreadConversations(
  rosterChannels: Channel[],
  dmChannels: Channel[],
  sessions: ChatSession[],
  placement: ChatPlacement,
): number {
  // FIR-4350 — count exactly what the Chat rail shows as unread. The rail's
  // rows badge on `unread_count` only; it renders no indicator for
  // `has_unread_activity` (mention-only smart-unread). Counting that here made
  // the sidebar badge promise an unread the user then found nowhere in Chat.

  // Named channels + groups: the rail's Channels group renders these from the
  // include-archived roster, so an inbox-archived one still shows in Chat and
  // still counts. (Groups map to the DM placement via conversationKindOfChannel.)
  const namedCount = rosterChannels.filter((c) => {
    if (c.kind !== "channel" && c.kind !== "group") return false;
    if (!showsInChat(placement, conversationKindOfChannel(c.kind))) return false;
    return c.unread_count > 0;
  }).length;
  // DMs: the rail's People section matches these from the NON-archived list, so
  // an inbox-archived DM (e.g. one snoozed as a reminder) is hidden from Chat.
  // It must not count here, or the badge promises an unread the user cannot find.
  const dmCount = showsInChat(placement, "dm")
    ? dmChannels.filter((c) => c.kind === "dm" && c.unread_count > 0).length
    : 0;
  const sessionCount = showsInChat(placement, "agent_chat")
    ? sessions.filter((s) => s.status !== "archived" && s.has_unread === true).length
    : 0;
  return namedCount + dmCount + sessionCount;
}

/** Unread conversation count for the sidebar Chat badge. */
export function useChatUnreadCount(wsId: string | null | undefined): number {
  const { placement } = useChatPlacement();
  // The roster query (include_archived) is the one the Chat page's SlackBlock
  // renders from — inbox-archiving hides a channel from the inbox feed, not
  // from Chat. Counting the plain inbox list here let an unread archived
  // channel sit in Chat without ever reaching the badge.
  const { data: rosterChannels = EMPTY_CHANNELS } = useQuery({
    ...channelRosterListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  // The non-archived list backs the rail's People section — count DMs from it so
  // an inbox-archived DM never reaches the badge without appearing in Chat.
  const { data: dmChannels = EMPTY_CHANNELS } = useQuery({
    ...channelListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: sessions = EMPTY_SESSIONS } = useQuery({
    ...chatSessionsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  return countUnreadConversations(rosterChannels, dmChannels, sessions, placement);
}

// Stable empty arrays — an inline `= []` default creates a new reference on
// every render while the query is disabled or loading.
const EMPTY_CHANNELS: Channel[] = [];
const EMPTY_SESSIONS: ChatSession[] = [];

// FIR-4350 — the Chat entry's badge: unread CONVERSATIONS only, and only the
// types placed in Chat. Counted client-side over the two queries the rail and
// both inbox implementations already fetch, so it adds no request.
//
// Counted from Channel.unread_count and ChatSession.has_unread — the same
// signals the rail rows badge on — so the badge can never claim an unread the
// user cannot find on the Chat page.
"use client";

import { useQuery } from "@tanstack/react-query";
import { channelRosterListOptions } from "@multica/core/channels";
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
  channels: Channel[],
  sessions: ChatSession[],
  placement: ChatPlacement,
): number {
  const channelCount = channels.filter((c) => {
    if (!showsInChat(placement, conversationKindOfChannel(c.kind))) return false;
    // FIR-4350 — count exactly what the Chat rail shows as unread. The rail's
    // rows badge on `unread_count` only; it renders no indicator for
    // `has_unread_activity` (mention-only smart-unread). Counting that here made
    // the sidebar badge promise an unread the user then found nowhere in Chat.
    return c.unread_count > 0;
  }).length;
  const sessionCount = showsInChat(placement, "agent_chat")
    ? sessions.filter((s) => s.status !== "archived" && s.has_unread === true).length
    : 0;
  return channelCount + sessionCount;
}

/** Unread conversation count for the sidebar Chat badge. */
export function useChatUnreadCount(wsId: string | null | undefined): number {
  const { placement } = useChatPlacement();
  // The roster query (include_archived) is the one the Chat page's SlackBlock
  // renders from — inbox-archiving hides a channel from the inbox feed, not
  // from Chat. Counting the plain inbox list here let an unread archived
  // channel sit in Chat without ever reaching the badge.
  const { data: channels = EMPTY_CHANNELS } = useQuery({
    ...channelRosterListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: sessions = EMPTY_SESSIONS } = useQuery({
    ...chatSessionsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  return countUnreadConversations(channels, sessions, placement);
}

// Stable empty arrays — an inline `= []` default creates a new reference on
// every render while the query is disabled or loading.
const EMPTY_CHANNELS: Channel[] = [];
const EMPTY_SESSIONS: ChatSession[] = [];

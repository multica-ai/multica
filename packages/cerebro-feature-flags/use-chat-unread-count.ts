// FIR-4350 — the Chat entry's badge: unread CONVERSATIONS only, and only the
// types placed in Chat. Counted client-side over the queries the rail and both
// inbox implementations already fetch, so it adds no request.
"use client";

import { useQuery } from "@tanstack/react-query";
import { channelListOptions, channelRosterListOptions } from "@multica/core/channels";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import type { Channel, ChatSession, InboxItem } from "@multica/core/types";
import { countUnreadConversations } from "./count-unread-conversations";
import { useChatPlacement } from "./use-chat-placement";
import { useFeatureFlag } from "./api";

export { countUnreadConversations } from "./count-unread-conversations";

/** Unread conversation count for the sidebar Chat badge. */
export function useChatUnreadCount(wsId: string | null | undefined): number {
  const { placement } = useChatPlacement();
  const threadSplitEnabled = useFeatureFlag("cerebro_inbox_thread_split");
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
  // FIR-4649 — same inbox feed the rail uses to split unread threads. No extra
  // request when the inbox (or Chat page) already has it warm.
  const { data: rawInbox = EMPTY_INBOX } = useQuery({
    ...inboxListOptions(wsId ?? ""),
    enabled: !!wsId && threadSplitEnabled,
  });
  return countUnreadConversations(
    rosterChannels,
    dmChannels,
    sessions,
    placement,
    rawInbox,
    threadSplitEnabled,
  );
}

// Stable empty arrays — an inline `= []` default creates a new reference on
// every render while the query is disabled or loading.
const EMPTY_CHANNELS: Channel[] = [];
const EMPTY_SESSIONS: ChatSession[] = [];
const EMPTY_INBOX: InboxItem[] = [];

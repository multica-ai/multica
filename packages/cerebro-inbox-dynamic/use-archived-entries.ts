// FIR-1645 — shared archived-inbox data source for both the full-screen
// archived VIEW (ArchivedInboxView, TECH-3541 #3) and the foldable Archived
// BLOCK (ArchivedInboxBlock). Pulls archived inbox notifications (paginated),
// archived chat sessions and archived channels/DMs (FIR-2791) and merges them
// into one time-sorted DynInboxEntry list, so the two surfaces never drift apart.
"use client";

import { useMemo } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  inboxKeys,
  ARCHIVED_INBOX_PAGE_SIZE,
  inboxItemSortTime,
} from "@multica/core/inbox/queries";
import { archivedChatSessionsOptions } from "@multica/core/chat/queries";
import { archivedChannelsOptions } from "@multica/cerebro-channels";
import type { Channel, ChatSession, InboxItem } from "@multica/core/types";
import type { DynInboxEntry } from "./section-filter";

// FIR-1686 — the sort time for an archived inbox notification: WHEN it was
// archived (archived_at, stamped server-side), so the archived surfaces default
// to most-recently-archived first. Falls back to the item's activity time on
// older servers (or rows archived before the column existed) that omit it.
export function archivedNotifSortTime(item: InboxItem): number {
  const archivedAt = item.archived_at ? Date.parse(item.archived_at) : NaN;
  return Number.isFinite(archivedAt) ? archivedAt : inboxItemSortTime(item);
}

// FIR-2791 — the sort time for an archived channel/DM row: its last message's
// time, falling back to the channel's own updated_at for empty conversations.
export function archivedChannelSortTime(channel: Channel): number {
  const lastMessage = channel.last_message?.created_at
    ? Date.parse(channel.last_message.created_at)
    : NaN;
  return Number.isFinite(lastMessage) ? lastMessage : new Date(channel.updated_at).getTime();
}

// FIR-2791 — pure merge of the three archived sources into one time-sorted
// entry list. Message notifications that belong to an archived channel/DM are
// folded into that channel's own row, so an archived group shows up exactly
// once instead of once per notification.
export function buildArchivedEntries(
  items: InboxItem[],
  archivedChats: ChatSession[],
  archivedChannels: Channel[],
): DynInboxEntry[] {
  const archivedChannelIds = new Set(archivedChannels.map((channel) => channel.id));
  const out: DynInboxEntry[] = [];
  for (const item of items) {
    if (item.issue_id && archivedChannelIds.has(item.issue_id)) continue;
    out.push({ kind: "notif", id: item.id, time: archivedNotifSortTime(item), item });
  }
  for (const session of archivedChats) {
    out.push({
      kind: "chat",
      id: session.id,
      time: new Date(session.updated_at).getTime(),
      session,
    });
  }
  for (const channel of archivedChannels) {
    out.push({
      kind: "channel",
      id: channel.id,
      time: archivedChannelSortTime(channel),
      channel,
    });
  }
  return out.sort((a, b) => b.time - a.time);
}

export interface UseArchivedInboxEntriesResult {
  entries: DynInboxEntry[];
  isLoading: boolean;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => void;
}

export function useArchivedInboxEntries(wsId: string): UseArchivedInboxEntriesResult {
  const archived = useInfiniteQuery({
    queryKey: inboxKeys.archivedList(wsId),
    queryFn: ({ pageParam }) =>
      api.listInbox({ archived: true, limit: ARCHIVED_INBOX_PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, allPages) =>
      lastPage.length === ARCHIVED_INBOX_PAGE_SIZE
        ? allPages.length * ARCHIVED_INBOX_PAGE_SIZE
        : undefined,
  });
  const { data: archivedChats = [] } = useQuery(archivedChatSessionsOptions(wsId));
  // FIR-2791 — archived channels/DMs/groups, so they can be found (and
  // unarchived) again from the Archived block and the archived view.
  const { data: archivedChannels = [] } = useQuery(archivedChannelsOptions(wsId));

  const items = useMemo(() => archived.data?.pages.flat() ?? [], [archived.data]);
  const entries = useMemo<DynInboxEntry[]>(
    () => buildArchivedEntries(items, archivedChats, archivedChannels),
    [items, archivedChats, archivedChannels],
  );

  return {
    entries,
    isLoading: archived.isLoading,
    hasNextPage: !!archived.hasNextPage,
    isFetchingNextPage: archived.isFetchingNextPage,
    fetchNextPage: () => archived.fetchNextPage(),
  };
}

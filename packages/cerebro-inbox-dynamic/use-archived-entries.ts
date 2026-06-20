// FIR-1645 — shared archived-inbox data source for both the full-screen
// archived VIEW (ArchivedInboxView, TECH-3541 #3) and the foldable Archived
// BLOCK (ArchivedInboxBlock). Pulls archived inbox notifications (paginated)
// plus archived chat sessions and merges them into one time-sorted
// DynInboxEntry list, so the two surfaces never drift apart.
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
import type { DynInboxEntry } from "./section-filter";

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

  const items = useMemo(() => archived.data?.pages.flat() ?? [], [archived.data]);
  const entries = useMemo<DynInboxEntry[]>(() => {
    const out: DynInboxEntry[] = [];
    for (const item of items) {
      out.push({ kind: "notif", id: item.id, time: inboxItemSortTime(item), item });
    }
    for (const session of archivedChats) {
      out.push({
        kind: "chat",
        id: session.id,
        time: new Date(session.updated_at).getTime(),
        session,
      });
    }
    return out.sort((a, b) => b.time - a.time);
  }, [items, archivedChats]);

  return {
    entries,
    isLoading: archived.isLoading,
    hasNextPage: !!archived.hasNextPage,
    isFetchingNextPage: archived.isFetchingNextPage,
    fetchNextPage: () => archived.fetchNextPage(),
  };
}

import {
  infiniteQueryOptions,
  queryOptions,
  type InfiniteData,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import type { InboxItem, InboxPage } from "@multica/core/types";
import { api } from "@/data/api";

/**
 * Inbox cache key factory.
 *
 * Names and shapes mirror web's active-view keys in
 * `packages/core/inbox/queries.ts` — `list`, `listPages`, `listLookup` — so
 * the cross-platform mental model stays the same. Web's page and lookup keys
 * end with the selected filters; the tab has none. Keying on wsId means
 * workspace switches naturally invalidate (TQ sees a new key and refetches).
 *
 * `list` is a prefix, not a cache of its own: the paged list and the per-group
 * lookups live under it, so every refresh of `list` (or `all`) reaches both.
 */
export const inboxKeys = {
  all: (wsId: string | null) => ["inbox", wsId] as const,
  list: (wsId: string | null) =>
    [...inboxKeys.all(wsId), "list"] as const,
  listPages: (wsId: string | null) => [...inboxKeys.list(wsId), "pages"] as const,
  listLookup: (wsId: string | null, groupId: string) =>
    [...inboxKeys.list(wsId), "lookup", groupId] as const,
  // Account-level, not workspace-scoped: one cache entry holding unread
  // counts for every workspace the user belongs to. Same key shape as web
  // (packages/core/inbox/queries.ts) so the mental model stays shared.
  unreadSummary: () => ["inbox", "unread-summary"] as const,
};

/** The inbox tab's list: one row per issue group, newest first, 50 a page. */
export const inboxPagesOptions = (wsId: string | null) =>
  infiniteQueryOptions({
    queryKey: inboxKeys.listPages(wsId),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      api.listInboxPage({ cursor: pageParam, signal }),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    enabled: !!wsId,
  });

/**
 * One group read directly, for a record that is on no loaded page — a deep
 * link, or a notice beyond what the tab has scrolled to. Kept out of the
 * pages' cursor chain so it never changes what the list shows. `groupId` is
 * the group key: `issue_id ?? id`.
 */
export const inboxLookupOptions = (wsId: string | null, groupId: string) =>
  queryOptions({
    queryKey: inboxKeys.listLookup(wsId, groupId),
    queryFn: ({ signal }) => api.listInboxPage({ groupId, signal }),
    enabled: !!wsId && !!groupId,
  });

/** Every cache shape under `inboxKeys.list`: the paged list and a lookup. */
export type InboxCache = InfiniteData<InboxPage> | InboxPage;

export function inboxCacheItems(data: InboxCache): InboxItem[] {
  return "pages" in data ? data.pages.flatMap((page) => page.items) : data.items;
}

/**
 * Apply one row transform to every inbox cache of the workspace, pages and
 * lookups alike, so a row patched on the list is patched in the sheet too.
 * Returns the caches as they were BEFORE the patch, for rollback.
 */
export function patchInboxCaches(
  qc: QueryClient,
  wsId: string | null,
  patch: (items: InboxItem[]) => InboxItem[],
): Array<[QueryKey, InboxCache | undefined]> {
  const snapshot = qc.getQueriesData<InboxCache>({
    queryKey: inboxKeys.list(wsId),
  });
  for (const [key, data] of snapshot) {
    if (!data) continue;
    qc.setQueryData<InboxCache>(
      key,
      "pages" in data
        ? {
            ...data,
            pages: data.pages.map((page) => ({
              ...page,
              items: patch(page.items),
            })),
          }
        : { ...data, items: patch(data.items) },
    );
  }
  return snapshot;
}

/**
 * Cross-workspace unread inbox summary — the source of the tab badge count.
 *
 * Gated on an active workspace because the endpoint resolves through the
 * workspace-member middleware, same as web's sidebar does.
 */
export const inboxUnreadSummaryOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: inboxKeys.unreadSummary(),
    queryFn: ({ signal }) => api.getInboxUnreadSummary({ signal }),
    enabled: !!wsId,
  });

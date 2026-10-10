import type { InfiniteData, QueryClient, QueryKey } from "@tanstack/react-query";
import type { InboxPage } from "../types/inbox";
import { EMPTY_INBOX_FILTERS, type InboxFilters } from "./filter-store";
import { infiniteQueryOptions, queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem, InboxWorkspaceUnread } from "../types";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  // Every cache of a view's rows nests under that view's prefix — the pages
  // and the deep-link lookups — so one prefix patches or refreshes the whole
  // view. Facets hold counts, not rows, so they sit
  // outside both prefixes.
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  listPages: (wsId: string) => [...inboxKeys.list(wsId), "pages"] as const,
  listLookup: (wsId: string) => [...inboxKeys.list(wsId), "lookup"] as const,
  listFacets: (wsId: string) => [...inboxKeys.all(wsId), "list-facets"] as const,
  archived: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  pages: (wsId: string) => [...inboxKeys.archived(wsId), "pages"] as const,
  lookup: (wsId: string) => [...inboxKeys.archived(wsId), "lookup"] as const,
  facets: (wsId: string) => [...inboxKeys.all(wsId), "archived-facets"] as const,
  // Account-level (not workspace-scoped): a single shared cache entry that
  // holds unread counts for every workspace the user belongs to.
  unreadSummary: () => ["inbox", "unread-summary"] as const,
};

function normalizedInboxFilters(filters: InboxFilters): InboxFilters {
  return { statuses: [...filters.statuses].sort(), priorities: [...filters.priorities].sort(),
    actors: [...filters.actors].sort(), unreadOnly: filters.unreadOnly };
}

/**
 * The active inbox, one issue group per row, loaded a page at a time. Filters
 * are applied by the server, so they stay correct beyond the loaded pages.
 */
export function inboxPagesOptions(wsId: string, filters: InboxFilters) {
  return infiniteQueryOptions({
    queryKey: [...inboxKeys.listPages(wsId), normalizedInboxFilters(filters)],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => api.listInboxPage(filters, { cursor: pageParam, signal }),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    retry: false,
  });
}

/** One active group by key, for a selection beyond the loaded pages. */
export function inboxLookupOptions(wsId: string, groupId: string, filters: InboxFilters = EMPTY_INBOX_FILTERS) {
  return queryOptions({
    queryKey: [...inboxKeys.listLookup(wsId), groupId, normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.listInboxPage(filters, { groupId, signal }),
    enabled: !!groupId,
    retry: false,
  });
}

export function inboxFacetsOptions(wsId: string, filters: InboxFilters) {
  return queryOptions({
    queryKey: [...inboxKeys.listFacets(wsId), normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.getInboxFacets(filters, signal),
    retry: false,
  });
}

export function archivedInboxPagesOptions(wsId: string, filters: InboxFilters) {
  return infiniteQueryOptions({
    queryKey: [...inboxKeys.pages(wsId), normalizedInboxFilters(filters)],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => api.listArchivedInboxPage(filters, { cursor: pageParam, signal }),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    retry: false,
  });
}

export function archivedInboxLookupOptions(wsId: string, groupId: string, filters: InboxFilters = EMPTY_INBOX_FILTERS) {
  return queryOptions({
    queryKey: [...inboxKeys.lookup(wsId), groupId, normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.listArchivedInboxPage(filters, { groupId, signal }),
    enabled: !!groupId,
    retry: false,
  });
}

export function archivedInboxFacetsOptions(wsId: string, filters: InboxFilters) {
  return queryOptions({
    queryKey: [...inboxKeys.facets(wsId), normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.getArchivedInboxFacets(filters, signal),
    retry: false,
  });
}

/** Any cache of one view's rows: a lookup page, or the pages. */
export type InboxCache = InboxPage | InfiniteData<InboxPage>;

/**
 * Apply an items patch to any row cache. A patch that returns its input
 * unchanged leaves the cache's identity intact, so an issue event that
 * touches no loaded row re-renders nothing.
 */
export function mapInboxCache(data: InboxCache, patch: (items: InboxItem[]) => InboxItem[]): InboxCache {
  if ("pages" in data) {
    let changed = false;
    const pages = data.pages.map((page) => {
      const items = patch(page.items);
      if (items === page.items) return page;
      changed = true;
      return { ...page, items };
    });
    return changed ? { ...data, pages } : data;
  }
  const items = patch(data.items);
  return items === data.items ? data : { ...data, items };
}

/** Every loaded row of any row cache. */
export function inboxCacheItems(data: InboxCache): InboxItem[] {
  return "pages" in data ? data.pages.flatMap((page) => page.items) : data.items;
}

/**
 * Patch every row cache under one view's prefix (`inboxKeys.list` or
 * `inboxKeys.archived`) and return the snapshot to roll back to.
 */
export function patchInboxCaches(qc: QueryClient, prefix: QueryKey, patch: (items: InboxItem[]) => InboxItem[]) {
  const snapshot = qc.getQueriesData<InboxCache>({ queryKey: prefix });
  for (const [key, data] of snapshot) {
    if (data) qc.setQueryData(key, mapInboxCache(data, patch));
  }
  return snapshot;
}

/**
 * Cross-workspace unread inbox summary. One cache entry shared across all
 * workspaces — the data is account-level, so switching workspaces does not
 * refetch it; only the derived "is this for another workspace" view changes.
 */
export function inboxUnreadSummaryOptions() {
  return queryOptions({
    queryKey: inboxKeys.unreadSummary(),
    queryFn: () => api.getInboxUnreadSummary(),
  });
}

/**
 * Whether any workspace OTHER than `currentWsId` has unread inbox items.
 * Drives the workspace-switcher dot: the active workspace's own unread is
 * already surfaced by the Inbox nav count, so it is excluded here to avoid a
 * duplicate signal.
 */
export function hasOtherWorkspaceUnread(
  summary: InboxWorkspaceUnread[],
  currentWsId: string | null | undefined,
): boolean {
  return summary.some((s) => s.workspace_id !== currentWsId && s.count > 0);
}

/**
 * Set of workspace ids that have unread inbox items. Lets the workspace
 * switcher dropdown mark WHICH workspace a pending message lives in (the
 * aggregate switcher dot only says "somewhere else"). Workspaces with a zero
 * count are excluded.
 */
export function unreadWorkspaceIds(summary: InboxWorkspaceUnread[]): Set<string> {
  return new Set(summary.filter((s) => s.count > 0).map((s) => s.workspace_id));
}

/**
 * Unread inbox count for one workspace within the cross-workspace summary.
 * A workspace with nothing unread is absent from the response entirely, so a
 * missing entry means zero rather than "not loaded yet".
 */
export function unreadCountForWorkspace(
  summary: InboxWorkspaceUnread[],
  wsId: string | null | undefined,
): number {
  if (!wsId) return 0;
  return summary.find((s) => s.workspace_id === wsId)?.count ?? 0;
}

/**
 * Unread inbox count for the given workspace — the number the sidebar nav
 * badge and the desktop dock badge render.
 *
 * Read from the cross-workspace summary, NOT from the inbox list. The summary
 * is one small server-computed row per workspace and the sidebar already
 * fetches it for the workspace-switcher dot, so the badge costs no request of
 * its own; deriving it from the full inbox list instead downloaded the entire
 * unbounded inbox on every app start just to render a number (MUL-6967).
 *
 * `GET /api/inbox/unread-count` is deliberately not the source: it counts raw
 * notification rows, while the inbox renders one row per issue. The summary
 * endpoint applies the same newest-per-issue rule `deduplicateInboxItems`
 * applies client-side, so this number matches the list the user sees.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: !!wsId,
    select: (summary: InboxWorkspaceUnread[]) =>
      unreadCountForWorkspace(summary, wsId),
  });
  return data ?? 0;
}

/**
 * Deduplicate inbox items by issue_id (one entry per issue, Linear-style).
 * Exported for consumers to use in useMemo — not in queryOptions select
 * (to avoid new array references on every cache update).
 */
export function deduplicateInboxItems(items: InboxItem[]): InboxItem[] {
  return groupInboxItemsByIssue(items.filter((i) => !i.archived));
}

/**
 * Same grouping for the archived sub-view. The `archived` filter is what makes
 * an optimistic unarchive drop the row out of the archived list immediately —
 * exactly mirroring how `deduplicateInboxItems`' filter drops an optimistically
 * archived row out of the main list.
 */
export function deduplicateArchivedInboxItems(items: InboxItem[]): InboxItem[] {
  return groupInboxItemsByIssue(items.filter((i) => i.archived));
}

function groupInboxItemsByIssue(items: InboxItem[]): InboxItem[] {
  const groups = new Map<string, InboxItem[]>();
  for (const item of items) {
    const key = item.issue_id ?? item.id;
    const group = groups.get(key) ?? [];
    group.push(item);
    groups.set(key, group);
  }
  const merged: InboxItem[] = [];
  for (const group of groups.values()) {
    group.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    const newest = group[0];
    if (!newest) continue;

    const commentId =
      newest.details?.comment_id ??
      group.find((item) => item.details?.comment_id)?.details?.comment_id;

    if (commentId && newest.details?.comment_id !== commentId) {
      merged.push({
        ...newest,
        details: { ...(newest.details ?? {}), comment_id: commentId },
      });
      continue;
    }

    merged.push(newest);
  }
  return merged.sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

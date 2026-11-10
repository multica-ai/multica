import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { ArchivedInboxPage } from "../types/inbox";
import { EMPTY_INBOX_FILTERS, archivedInboxQuery, type ArchivedInboxFilters } from "./filters";
import { groupInboxItemsByIssue } from "./group";

export {
  inboxGroupUnread,
  isInboxNotice,
  summarizeInboxGroups,
  type InboxGroupSummary,
  type InboxGroupUnread,
} from "./group";
import { infiniteQueryOptions, keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem, InboxWorkspaceUnread } from "../types";
import { focusManager } from "@tanstack/react-query";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  archived: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  pages: (wsId: string) => [...inboxKeys.archived(wsId), "pages"] as const,
  lookup: (wsId: string) => [...inboxKeys.archived(wsId), "lookup"] as const,
  facets: (wsId: string) => [...inboxKeys.all(wsId), "archived-facets"] as const,
  // Account-level (not workspace-scoped): a single shared cache entry that
  // holds unread counts for every workspace the user belongs to.
  unreadSummary: () => ["inbox", "unread-summary"] as const,
};

export function inboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: () => api.listInbox(),
  });
}

/**
 * @deprecated Legacy array endpoint, capped at 200 groups. New archive
 * consumers must use archivedInboxPagesOptions for the complete archive.
 * Retained for compatibility with legacy cache consumers.
 */
export function archivedInboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.archived(wsId),
    queryFn: () => api.listArchivedInbox(),
  });
}

// Keyed by the request parameters themselves: an `agent` selection carried in
// from the active inbox never changes the key, and the relative windows
// (received, due date) re-key when the day turns.
function normalizedInboxFilters(filters: ArchivedInboxFilters): Record<string, string> {
  return archivedInboxQuery(filters);
}

// A filter change re-keys the archive. Keep the previous result while the new
// one loads, so the page does not drop to its skeleton and unmount the open
// filter menu. Only within the same workspace: another workspace's rows or
// counts must never stand in.
function isSameWorkspace(previousKey: readonly unknown[] | undefined, wsId: string) {
  return previousKey?.[1] === wsId;
}

export function archivedInboxPagesOptions(wsId: string, filters: ArchivedInboxFilters) {
  return infiniteQueryOptions({
    queryKey: [...inboxKeys.pages(wsId), normalizedInboxFilters(filters)],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => api.listArchivedInboxPage(filters, { cursor: pageParam, signal }),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    placeholderData: (previousData, previousQuery) =>
      isSameWorkspace(previousQuery?.queryKey, wsId) ? keepPreviousData(previousData) : undefined,
    retry: false,
  });
}

export function archivedInboxLookupOptions(wsId: string, groupId: string, filters: ArchivedInboxFilters = EMPTY_INBOX_FILTERS) {
  const prefix = inboxKeys.lookup(wsId);
  return queryOptions({
    queryKey: [...prefix, groupId, normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.listArchivedInboxPage(filters, { groupId, signal }),
    enabled: !!groupId,
    retry: false,
    // A filter change re-asks for the same group. Its last answer stays up
    // meanwhile, so the open row is judged by the new filters at once instead
    // of the selection hanging on the request.
    placeholderData: (previousData, previousQuery) =>
      isSameWorkspace(previousQuery?.queryKey, wsId) && previousQuery?.queryKey[prefix.length] === groupId
        ? previousData
        : undefined,
  });
}

export function archivedInboxFacetsOptions(wsId: string, filters: ArchivedInboxFilters) {
  return queryOptions({
    queryKey: [...inboxKeys.facets(wsId), normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.getArchivedInboxFacets(filters, signal),
    placeholderData: (previousData, previousQuery) =>
      isSameWorkspace(previousQuery?.queryKey, wsId) ? keepPreviousData(previousData) : undefined,
    retry: false,
  });
}

export type ArchivedInboxCache = InboxItem[] | ArchivedInboxPage | InfiniteData<ArchivedInboxPage>;

export function mapArchivedInboxCache(data: ArchivedInboxCache, patch: (items: InboxItem[]) => InboxItem[]): ArchivedInboxCache {
  if (Array.isArray(data)) return patch(data);
  if ("pages" in data) return { ...data, pages: data.pages.map((page) => ({ ...page, items: patch(page.items) })) };
  return { ...data, items: patch(data.items) };
}

export function patchArchivedInboxCaches(qc: QueryClient, wsId: string, patch: (items: InboxItem[]) => InboxItem[]) {
  const snapshot = qc.getQueriesData<ArchivedInboxCache>({ queryKey: inboxKeys.archived(wsId) });
  for (const [key, data] of snapshot) {
    if (data) qc.setQueryData(key, mapArchivedInboxCache(data, patch));
  }
  return snapshot;
}

/**
 * Cross-workspace unread inbox summary. One cache entry shared across all
 * workspaces — the data is account-level, so switching workspaces does not
 * refetch it; only the derived "is this for another workspace" view changes.
 *
 * Visibility gating is the default, including for direct observers such as
 * the search palette. `refetchOnWindowFocus` re-reads an invalidated summary
 * once when the page is shown again; with `staleTime: Infinity` a page that
 * missed no event is not stale and makes no request. The dock badge overrides
 * `enabled` through `inboxUnreadSummaryEnabled` to stay live while hidden.
 */
export function inboxUnreadSummaryOptions() {
  return queryOptions({
    queryKey: inboxKeys.unreadSummary(),
    queryFn: () => api.getInboxUnreadSummary(),
    enabled: () => focusManager.isFocused(),
    refetchOnWindowFocus: true,
  });
}

/**
 * `enabled` for an observer of the unread summary: fetch only while the page
 * is on screen. Every open tab is its own client and receives every inbox
 * event, so each event cost one summary request per open tab, and each request
 * recomputes the newest item per issue over the user's whole active inbox. A
 * hidden tab renders nothing that reads the summary, and with no enabled
 * observer left an invalidation marks the query without refetching it.
 *
 * `whileHidden` is for a reader that stays on screen while the page is hidden:
 * the desktop dock badge of a minimized window.
 */
export function inboxUnreadSummaryEnabled(
  wsId: string | null | undefined,
  { whileHidden = false }: { whileHidden?: boolean } = {},
) {
  return () => !!wsId && (whileHidden || focusManager.isFocused());
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
 * How many issues wait on the user in the given workspace's Inbox, read or
 * not — the number the Work rail badge, the Inbox row and the desktop dock
 * badge render. Only acting on an issue or archiving it lowers it.
 *
 * Read from the cross-workspace summary, NOT from the inbox list. The summary
 * is one small server-computed row per workspace and the sidebar already
 * fetches it for the workspace-switcher dot, so the badge costs no request of
 * its own; deriving it from `listInbox()` instead downloaded the entire
 * unbounded inbox on every app start just to render a number (MUL-6967).
 *
 * `GET /api/inbox/unread-count` is deliberately not the source: it counts raw
 * notification rows, while the inbox renders one row per issue. The summary
 * endpoint counts issues, over the notification types the list shows, so
 * this number matches the list the user sees.
 */
export function useInboxUnreadCount(
  wsId: string | null | undefined,
  options?: { whileHidden?: boolean },
): number {
  const { data } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: inboxUnreadSummaryEnabled(wsId, options),
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

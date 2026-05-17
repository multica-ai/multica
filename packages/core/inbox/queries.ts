// CEREBRO-PATCH(core-inbox-queries): cerebro modification of upstream file
import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem } from "../types";

// CEREBRO-PATCH(inbox-archive-pagination): keep archived inbox first render
// bounded; the active inbox query shape is unchanged.
export const ARCHIVED_INBOX_PAGE_SIZE = 50;

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  archivedList: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  activeIssueTasks: (wsId: string) =>
    [...inboxKeys.all(wsId), "active-issue-tasks"] as const,
};

// Cross-workspace key, kept separate from inboxKeys (which all carry a wsId)
// so it doesn't get caught by per-workspace invalidations during workspace
// switching. Drives the OS app-icon badge.
export const appBadgeKeys = {
  unreadCount: () => ["app-badge", "unread-count"] as const,
};

export function appBadgeUnreadCountOptions() {
  return queryOptions({
    queryKey: appBadgeKeys.unreadCount(),
    queryFn: () => api.getMyTotalUnreadInboxCount(),
  });
}

export function inboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: () => api.listInbox(),
  });
}

export function inboxArchivedListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.archivedList(wsId),
    queryFn: () =>
      api.listInbox({ archived: true, limit: ARCHIVED_INBOX_PAGE_SIZE, offset: 0 }),
  });
}

/**
 * Issue IDs in the workspace with at least one in-flight task. Used by the
 * inbox list to render an "agent is working" indicator on each row.
 */
export function activeIssueTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.activeIssueTasks(wsId),
    queryFn: () => api.listActiveIssueTasks(),
    staleTime: Infinity,
  });
}

/**
 * Unread inbox count for the given workspace, aligned with what the inbox
 * list UI renders: archived items excluded, then deduplicated by issue so a
 * single issue with three unread notifications counts once.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    queryKey: inboxKeys.list(wsId ?? ""),
    queryFn: () => api.listInbox(),
    enabled: !!wsId,
    select: (items: InboxItem[]) =>
      deduplicateInboxItems(items).filter((i) => !i.read).length,
  });
  return data ?? 0;
}

/**
 * Deduplicate inbox items by issue_id (one entry per issue, Linear-style).
 * Exported for consumers to use in useMemo — not in queryOptions select
 * (to avoid new array references on every cache update).
 */
export function deduplicateInboxItems(items: InboxItem[]): InboxItem[] {
  const active = items.filter((i) => !i.archived);
  const groups = new Map<string, InboxItem[]>();
  for (const item of active) {
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
    if (group[0]) merged.push(group[0]);
  }
  return merged.sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

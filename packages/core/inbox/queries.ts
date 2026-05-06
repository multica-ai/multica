// CEREBRO-PATCH(core-inbox-queries): cerebro modification of upstream file
import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem } from "../types";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  archivedList: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  folderList: (wsId: string, folderId: string) =>
    [...inboxKeys.all(wsId), "folder", folderId] as const,
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
    queryFn: () => api.listInbox({ archived: true }),
  });
}

export function inboxListInFolderOptions(wsId: string, folderId: string) {
  return queryOptions({
    queryKey: inboxKeys.folderList(wsId, folderId),
    queryFn: () => api.listInbox({ folder: folderId }),
    enabled: !!folderId,
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

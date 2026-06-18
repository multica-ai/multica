import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem } from "../types";
import { getInboxItemSelectionKey } from "./channel";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
};

export function inboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: () => api.listInbox(),
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
    const key = getInboxItemSelectionKey(item);
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
    const representative = group[0];
    if (!representative) continue;

    // If the newest notification has no comment anchor (e.g. task_completed),
    // find the most recent notification in the group that does.  Without this,
    // the inbox detail opens in "latest" mode (no around-mode) and can end up
    // showing only recent activity entries while the user's chat messages are
    // hidden behind a "show older" page load.
    if (!representative.details?.comment_id) {
      const withComment = group.find((i) => i.details?.comment_id);
      if (withComment?.details?.comment_id) {
        merged.push({
          ...representative,
          details: {
            ...(representative.details ?? {}),
            comment_id: withComment.details.comment_id,
          },
        });
        continue;
      }
    }

    merged.push(representative);
  }
  return merged.sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

// CEREBRO-PATCH(core-inbox-ws-updaters): cerebro modification of upstream file
import type { QueryClient } from "@tanstack/react-query";
import { appBadgeKeys, inboxKeys } from "./queries";
import { notificationsKeys } from "@multica/cerebro-notifications/core/queries";
import type { InboxItem, IssueStatus } from "../types";

export function onInboxNew(
  qc: QueryClient,
  wsId: string,
  item: InboxItem,
) {
  // Route decides which list got a new item. Both are small, so an extra
  // invalidate is cheaper than missing the right cache.
  if (item.route === "notifications") {
    qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
    qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
  } else {
    qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    qc.invalidateQueries({ queryKey: appBadgeKeys.unreadCount() });
  }
}

export function onInboxIssueStatusChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.map((i) =>
      i.issue_id === issueId ? { ...i, issue_status: status } : i,
    ),
  );
  qc.setQueryData<InboxItem[]>(notificationsKeys.list(wsId), (old) =>
    old?.map((i) =>
      i.issue_id === issueId ? { ...i, issue_status: status } : i,
    ),
  );
}

// Mirrors the DB-level ON DELETE CASCADE on inbox_item.issue_id: when an issue
// is deleted, all inbox items that referenced it are gone server-side, so drop
// them from the cache too.
export function onInboxIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.filter((i) => i.issue_id !== issueId),
  );
}

export function onInboxInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
  qc.invalidateQueries({ queryKey: appBadgeKeys.unreadCount() });
}

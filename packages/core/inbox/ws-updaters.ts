import type { QueryClient } from "@tanstack/react-query";
import { appBadgeKeys, inboxKeys } from "./queries";
import { notificationsKeys } from "../notifications/queries";
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

export function onInboxInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
  qc.invalidateQueries({ queryKey: appBadgeKeys.unreadCount() });
}

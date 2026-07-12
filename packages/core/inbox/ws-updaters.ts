import type { QueryClient } from "@tanstack/react-query";
import { inboxKeys } from "./queries";
import type { InboxItem, IssueStatus } from "../types";

export function onInboxNew(
  qc: QueryClient,
  wsId: string,
  _item: InboxItem,
) {
  // Use invalidateQueries instead of setQueryData — triggers a refetch that
  // reliably notifies all observers. The inbox list is small so this is cheap.
  qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
}

export function patchInboxIssueStatus(
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
}

export function onInboxIssueStatusChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  patchInboxIssueStatus(qc, wsId, issueId, status);
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
}

// Refresh the cross-workspace unread summary (workspace-switcher dot). The
// summary spans every workspace, so it is invalidated on ANY inbox event
// regardless of which workspace the event came from — including read/archive
// events from a workspace other than the active one, which the workspace-
// scoped list invalidation cannot reach.
export function onInboxSummaryInvalidate(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}

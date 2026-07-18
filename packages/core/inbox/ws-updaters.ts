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
  //
  // Both lists: a new notification on an ARCHIVED issue puts that issue back in
  // the main inbox, which means it must also leave the archived list. The
  // server owns that split (ListArchivedInboxItems excludes issues with an
  // active row), so refetching both is what keeps them mutually exclusive.
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

export function patchInboxIssueStatus(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  const patch = (old: InboxItem[] | undefined) =>
    old?.map((i) => (i.issue_id === issueId ? { ...i, issue_status: status } : i));
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), patch);
  // Archived rows render the same status icon, so they need the same patch.
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), patch);
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
// them from the cache too — from the archived list as well, which holds rows
// for the same issues.
export function onInboxIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  const drop = (old: InboxItem[] | undefined) =>
    old?.filter((i) => i.issue_id !== issueId);
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), drop);
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), drop);
}

// Refresh both the main and archived lists. Every inbox event can move an item
// across that boundary (archive, unarchive, or a new notification reviving an
// archived issue), and the split is decided server-side, so the two are always
// invalidated together.
export function onInboxInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

// Refresh the cross-workspace unread summary (workspace-switcher dot). The
// summary spans every workspace, so it is invalidated on ANY inbox event
// regardless of which workspace the event came from — including read/archive
// events from a workspace other than the active one, which the workspace-
// scoped list invalidation cannot reach.
export function onInboxSummaryInvalidate(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}

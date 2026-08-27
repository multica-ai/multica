import type { QueryClient } from "@tanstack/react-query";
import { inboxKeys } from "./queries";
import type { InboxItem, IssuePriority, IssueStatus } from "../types";

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

export function patchInboxIssueProjection(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: { status?: IssueStatus; priority?: IssuePriority },
) {
  const project = (old: InboxItem[] | undefined) => {
    if (!old) return old;
    let changed = false;
    const next = old.map((item) => {
      if (item.issue_id !== issueId) return item;
      changed = true;
      return {
        ...item,
        ...(patch.status !== undefined
          ? { issue_status: patch.status }
          : {}),
        // Do not manufacture the projection on data returned by an older
        // backend. Capability detection relies on `undefined` continuing to
        // mean "this endpoint version does not provide issue_priority".
        ...(patch.priority !== undefined && item.issue_priority !== undefined
          ? { issue_priority: patch.priority }
          : {}),
      };
    });
    return changed ? next : old;
  };
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), project);
  // Archived rows expose the same issue fields and filter controls.
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), project);
}

export function patchInboxIssueStatus(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  patchInboxIssueProjection(qc, wsId, issueId, { status });
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

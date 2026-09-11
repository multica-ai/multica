import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { inboxKeys } from "./queries";
import type { InboxItem, IssuePriority, IssueStatus } from "../types";

// Re-read a query because the server changed — in a way that is never answered
// by a request that was already on the wire before the change.
//
// Plain invalidation does not give that guarantee. TanStack only cancels an
// in-flight request on invalidation once the query already holds data —
// `Query.fetch` guards that branch on `state.data !== undefined` and otherwise
// hands back the request already in flight:
//
//     if (this.state.data !== undefined && fetchOptions?.cancelRefetch) {
//       this.cancel({ silent: true })
//     } else if (this.#retryer) {
//       return this.#retryer.promise      // ← the pre-change request
//     }
//
// That branch exists to dedupe concurrent mounts, not to carry freshness. So a
// change that lands during a query's FIRST load is answered by the pre-change
// response, which resolves successfully and clears `isInvalidated`; with
// `staleTime: Infinity` and no refetch on focus, nothing asks again. Cancelling
// first makes a refresh behave the same whether or not the query has loaded.
//
// Every inbox cache is refreshed through here — both lists and the unread
// summary. They are rendered side by side (the badge next to the rows it
// counts), so a hole in either one shows up as the two disagreeing (MUL-6967).
async function refreshInboxQuery(qc: QueryClient, queryKey: QueryKey) {
  await qc.cancelQueries({ queryKey });
  await qc.invalidateQueries({ queryKey });
}

export async function onInboxNew(
  qc: QueryClient,
  wsId: string,
  _item: InboxItem,
) {
  // Refetch instead of setQueryData, so every observer is notified.
  //
  // Both lists: a new notification on an ARCHIVED issue puts that issue back in
  // the main inbox, which means it must also leave the archived list. The
  // server owns that split (ListArchivedInboxItems excludes issues with an
  // active row), so refetching both is what keeps them mutually exclusive.
  await onInboxInvalidate(qc, wsId);
}

// An issue event only patches known rows; it cannot fulfill a pending inbox
// refresh. setQueryData clears isInvalidated even when the updater returns the
// same array. Preserve it so an inactive list still refetches on its next mount
// (MUL-7286). Do not restart an active fetch or introduce a fetch per issue event.
function patchInboxLists(
  qc: QueryClient,
  wsId: string,
  patch: (items: InboxItem[]) => InboxItem[],
) {
  for (const queryKey of [inboxKeys.list(wsId), inboxKeys.archived(wsId)]) {
    const invalidated = qc.getQueryState(queryKey)?.isInvalidated === true;
    qc.setQueryData<InboxItem[]>(queryKey, (old) => {
      if (!old) return undefined;
      const next = patch(old);
      return next === old ? undefined : next;
    });
    if (invalidated) {
      void qc.invalidateQueries({ queryKey, exact: true, refetchType: "none" });
    }
  }
}

export function patchInboxIssueProjection(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: { status?: IssueStatus; priority?: IssuePriority },
) {
  const project = (old: InboxItem[]) => {
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
  // Archived rows expose the same issue fields and filter controls.
  patchInboxLists(qc, wsId, project);
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
//
// Dropping unread rows changes the unread badge, which reads the server-side
// summary rather than these lists, so the summary is refreshed here too. It
// has to happen inside this updater and not at the call site: deletion is an
// `issue:*` event, so no `inbox:*` handler runs to pick it up, and the summary
// query is `staleTime: Infinity` with no refetch on focus — nothing else would
// ever correct it, leaving the badge stuck above an empty inbox (MUL-6967).
export async function onInboxIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  patchInboxLists(qc, wsId, (items) =>
    items.filter((i) => i.issue_id !== issueId),
  );
  await onInboxSummaryInvalidate(qc);
}

// Whether a request for either inbox list is out (paused ones included).
export function isInboxListRequestInFlight(
  qc: QueryClient,
  wsId: string,
): boolean {
  return qc
    .getQueryCache()
    .findAll({ queryKey: inboxKeys.all(wsId) })
    .some((query) => query.state.fetchStatus !== "idle");
}

// An optimistic issue write patches the status / priority on inbox rows, so it
// first cancels the lists' in-flight requests: a response read before the write
// would land on top of the patch. Returns whether a request was interrupted.
//
// The interrupted request may be the only one carrying a server change (an
// `inbox:new`, a reconnect), and nothing asks again. The cancel can also drop
// the invalidation mark: TanStack reverts to its pre-fetch snapshot, and a
// `setQueryData` during the fetch overwrites that snapshot with a
// non-invalidated state. So a write that interrupted a request owes the lists
// a re-read once the writes settle (MUL-7286). Writes that interrupt nothing
// stay request-free.
export function cancelInboxLists(qc: QueryClient, wsId: string): boolean {
  const interrupted = isInboxListRequestInFlight(qc, wsId);
  void qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
  return interrupted;
}

// THE entry point for refreshing the workspace's inbox lists — main and
// archived. Every inbox event can move an item across that boundary (archive,
// unarchive, or a new notification reviving an archived issue), and the split
// is decided server-side, so the two are always refreshed together.
//
// Cancels first, like the summary refresh below, and for the same reason: the
// Inbox page is the only observer that fetches the list, so its first visit
// downloads a large, unbounded list while notifications keep arriving. After
// that the cache outlives the page — tab titles hold disabled observers on it
// (`useTabPresentation`) — so a return reuses it and refetches only while it
// is still marked invalidated.
export async function onInboxInvalidate(qc: QueryClient, wsId: string) {
  await refreshInboxQuery(qc, inboxKeys.all(wsId));
}

// THE entry point for refreshing the cross-workspace unread summary — the
// workspace-switcher dot and the Inbox unread badge. Every writer goes through
// here: inbox mutations, inbox events, issue deletion, and reconnect. The
// summary spans every workspace, so it is refreshed on ANY inbox event
// regardless of which workspace the event came from — including read/archive
// events from a workspace other than the active one, which the workspace-
// scoped list refresh cannot reach. Cancels first; see `refreshInboxQuery`.
export async function onInboxSummaryInvalidate(qc: QueryClient) {
  await refreshInboxQuery(qc, inboxKeys.unreadSummary());
}

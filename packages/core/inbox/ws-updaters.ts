import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { coalescedRefresh, inboxRowFromPush } from "./incremental";
import { archivedInboxCacheItems, inboxKeys, mapArchivedInboxCache, type ArchivedInboxCache } from "./queries";
import { useInboxSelectionStore } from "./selection-store";
import type { InboxItem, IssueAssigneeType, IssuePriority, IssueStatus, SnoozedInboxIssue } from "../types";

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

// A new notification is written into the loaded main list rather than
// re-reading every inbox list: each one used to re-download the whole inbox in
// every open client. It is written only when the result is certain, and
// otherwise the lists are re-read through a coalescing gate:
// - The main list is loaded, owes no re-read, and no inbox request is out. A
//   response already on the wire can predate the row and would land on top of
//   it (see refreshInboxQuery).
// - The issue fields the row mirrors are known (`inboxRowFromPush`).
// - The issue is not snoozed. A notification that ends a snooze brings the
//   issue's earlier rows back too, and only the server has them. One that
//   joins a snooze arrives as inbox:snoozed instead, so Snoozed is untouched.
//
// A notification on an archived issue puts the issue back in the inbox, and
// the archive excludes an issue with an active row (ListArchivedInboxItems),
// so its loaded archived rows go. Which archived groups and facet counts match
// is the server's call: those are marked stale and re-read when they are next
// mounted, not now.
export async function onInboxNew(
  qc: QueryClient,
  wsId: string,
  item: InboxItem,
) {
  const listKey = inboxKeys.list(wsId);
  const list = qc.getQueryState<InboxItem[]>(listKey);
  const issueId = item.issue_id;
  const snoozed = issueId
    ? qc.getQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId))?.some((entry) => entry.issue_id === issueId)
    : false;
  if (!list?.data || list.isInvalidated || snoozed || isInboxListRequestInFlight(qc, wsId)) {
    return refreshInboxListsCoalesced(qc, wsId);
  }
  if (list.data.some((row) => row.id === item.id)) return;
  const archived = qc
    .getQueriesData<ArchivedInboxCache>({ queryKey: inboxKeys.archived(wsId) })
    .map(([queryKey, data]) => ({ queryKey, items: data ? archivedInboxCacheItems(data) : [] }));
  const row =
    inboxRowFromPush(item, list.data) ??
    inboxRowFromPush(item, archived.flatMap((cache) => cache.items));
  if (!row) return refreshInboxListsCoalesced(qc, wsId);
  qc.setQueryData<InboxItem[]>(listKey, (old) => (old ? [row, ...old] : old));
  if (!issueId) return;
  const revived = archived.filter((cache) => cache.items.some((i) => i.issue_id === issueId));
  patchInboxQueries(qc, revived.map((cache) => cache.queryKey), (items) =>
    items.filter((i) => i.issue_id !== issueId),
  );
  for (const queryKey of [inboxKeys.archived(wsId), inboxKeys.facets(wsId)]) {
    void qc.invalidateQueries({ queryKey, refetchType: "none" });
  }
}

// The fallback for an `inbox:new` that cannot be written locally. A burst of
// them must not stack one full re-read each: the client cancels the
// request it replaces, but the server finishes the query regardless.
function refreshInboxListsCoalesced(qc: QueryClient, wsId: string) {
  return coalescedRefresh(qc, `lists:${wsId}`, () => onInboxInvalidate(qc, wsId));
}

function archivedInboxQueryKeys(qc: QueryClient, wsId: string): QueryKey[] {
  return qc.getQueryCache().findAll({ queryKey: inboxKeys.archived(wsId) }).map((query) => query.queryKey);
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
  patchInboxQueries(qc, [inboxKeys.list(wsId), ...archivedInboxQueryKeys(qc, wsId)], patch);
}

function patchInboxQueries(
  qc: QueryClient,
  queryKeys: readonly QueryKey[],
  patch: (items: InboxItem[]) => InboxItem[],
) {
  for (const queryKey of queryKeys) {
    const invalidated = qc.getQueryState(queryKey)?.isInvalidated === true;
    qc.setQueryData<ArchivedInboxCache>(queryKey, (old) => {
      if (!old) return undefined;
      const next = mapArchivedInboxCache(old, patch);
      return next === old ? undefined : next;
    });
    if (invalidated) {
      void qc.invalidateQueries({ queryKey, exact: true, refetchType: "none" });
    }
  }
}

/** Issue fields inbox rows mirror for display and filtering. */
export interface InboxIssueProjectionPatch {
  status?: IssueStatus;
  priority?: IssuePriority;
  project_id?: string | null;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  due_date?: string | null;
  label_ids?: string[];
}

export function patchInboxIssueProjection(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: InboxIssueProjectionPatch,
) {
  const project = (old: InboxItem[]) => {
    let changed = false;
    const next = old.map((item) => {
      if (item.issue_id !== issueId) return item;
      changed = true;
      // Do not manufacture a projection on data returned by an older
      // backend. Capability detection relies on `undefined` continuing to
      // mean "this endpoint version does not provide the field".
      const hasIssueFields = item.issue_label_ids !== undefined;
      return {
        ...item,
        ...(patch.status !== undefined
          ? { issue_status: patch.status }
          : {}),
        ...(patch.priority !== undefined && item.issue_priority !== undefined
          ? { issue_priority: patch.priority }
          : {}),
        ...(hasIssueFields && patch.project_id !== undefined
          ? { issue_project_id: patch.project_id }
          : {}),
        ...(hasIssueFields && patch.assignee_type !== undefined
          ? { issue_assignee_type: patch.assignee_type }
          : {}),
        ...(hasIssueFields && patch.assignee_id !== undefined
          ? { issue_assignee_id: patch.assignee_id }
          : {}),
        ...(hasIssueFields && patch.due_date !== undefined
          ? { issue_due_date: patch.due_date }
          : {}),
        ...(hasIssueFields && patch.label_ids !== undefined
          ? { issue_label_ids: patch.label_ids }
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
  // The issue cache coordinator owns membership invalidation after commit.
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
// query is `staleTime: Infinity`, re-read only once invalidated — nothing else
// would ever correct it, leaving the badge stuck above an empty inbox (MUL-6967).
export async function onInboxIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  patchInboxLists(qc, wsId, (items) =>
    items.filter((i) => i.issue_id !== issueId),
  );
  // A row kept in place for the open issue must not bring a deleted issue back.
  useInboxSelectionStore.getState().forgetRows(wsId, [issueId]);
  await Promise.all([
    onInboxSummaryInvalidate(qc),
    refreshInboxQuery(qc, inboxKeys.pages(wsId)),
    refreshInboxQuery(qc, inboxKeys.lookup(wsId)),
    refreshInboxQuery(qc, inboxKeys.facets(wsId)),
  ]);
}

// The delete mutations' own inbox cleanup. The deleting client cannot count
// on its `issue:deleted` echo arriving (a proxied deployment can drop the
// socket silently), and the inbox list it loaded before the delete keeps rows
// whose issue no longer exists — clicking one routes to a 404. Mirrors
// `onInboxIssueDeleted`'s DB-cascade prune for a batch, plus the mutation
// write discipline the update mutations already follow: a list response that
// was on the wire before the delete lands on top of the prune and
// resurrects the rows, so the lists are cancelled first and owed a re-read
// when that interrupted a request (see `cancelInboxLists`).
export async function onInboxIssuesDeleted(
  qc: QueryClient,
  wsId: string,
  issueIds: string[],
) {
  if (issueIds.length === 0) return;
  const interrupted = cancelInboxLists(qc, wsId);
  const deleted = new Set(issueIds);
  patchInboxLists(qc, wsId, (items) => {
    const next = items.filter((i) => i.issue_id === null || !deleted.has(i.issue_id));
    return next.length === items.length ? items : next;
  });
  useInboxSelectionStore.getState().forgetRows(wsId, issueIds);
  const refreshes: Promise<unknown>[] = [
    onInboxSummaryInvalidate(qc),
    refreshInboxQuery(qc, inboxKeys.pages(wsId)),
    refreshInboxQuery(qc, inboxKeys.lookup(wsId)),
    refreshInboxQuery(qc, inboxKeys.facets(wsId)),
  ];
  if (interrupted) refreshes.push(onInboxInvalidate(qc, wsId));
  await Promise.all(refreshes);
}

// An issue field the archive filters and counts on changed. Loaded rows are
// patched in place, but which groups match and the facet counts are decided
// by the server: an issue that newly matches a filter would never appear, and
// with `staleTime: Infinity` an empty result would stay empty. Marked
// synchronously so an archive that is not mounted re-reads on its next visit.
export async function refreshArchivedInboxMatches(qc: QueryClient, wsId: string) {
  const keys = [inboxKeys.pages(wsId), inboxKeys.lookup(wsId), inboxKeys.facets(wsId)];
  for (const queryKey of keys) void qc.invalidateQueries({ queryKey, refetchType: "none" });
  await Promise.all(keys.map((queryKey) => refreshInboxQuery(qc, queryKey)));
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

// The summary refresh for `inbox:new`, which arrives in bursts (an agent at
// work notifies on every step). One request at a time, and the events that
// arrive meanwhile share one follow-up. The server computes the summary over
// the whole inbox, so the count is not adjusted locally.
export function onInboxNewSummaryInvalidate(qc: QueryClient) {
  return coalescedRefresh(qc, "unread-summary", () => onInboxSummaryInvalidate(qc));
}

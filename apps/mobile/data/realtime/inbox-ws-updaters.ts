/**
 * Mobile inbox cache patchers. Mirrors `packages/core/inbox/ws-updaters.ts`
 * (per CLAUDE.md "Mobile-owned updaters" — copy the design, don't import:
 * key factory binding + cache shape can drift independently).
 *
 * Two cross-cutting events that change inbox state without firing an
 * `inbox:*` event:
 *   - `issue:updated` carrying a new status → the inbox row's StatusIcon
 *     must update inline. Without this patch the row keeps showing the
 *     prior status until the next inbox event triggers a full refetch.
 *   - `issue:deleted` → all inbox items pointing at that issue are gone
 *     server-side (FK ON DELETE CASCADE in the DB); the cache should drop
 *     them too, otherwise tapping an inbox row navigates to a 404 issue.
 *
 * Listing-level only; use-inbox-realtime wires these into the WS layer.
 */
import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { InboxItem, IssueStatus } from "@multica/core/types";
import { inboxKeys } from "@/data/queries/inbox";

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

/**
 * Re-read a query because the server changed, in a way a request already on
 * the wire cannot answer. Mirrors `refreshInboxQuery` in
 * packages/core/inbox/ws-updaters.ts — including why the cancel comes first.
 *
 * TanStack only cancels an in-flight request on invalidation once the query
 * already holds data (`Query.fetch` guards that branch on
 * `state.data !== undefined`, and otherwise returns the in-flight promise). A
 * change during a query's FIRST load is therefore answered by the pre-change
 * response, which resolves successfully and clears `isInvalidated`, with
 * nothing scheduled to ask again. Cancelling first makes a refresh behave the
 * same whether or not the query has loaded yet (MUL-6967).
 *
 * Both inbox caches go through this, not only the badge: the tab badge and the
 * list it counts are separate reads, so a hole in either one surfaces as the
 * badge disagreeing with the rows on screen.
 */
async function refreshInboxQuery(qc: QueryClient, queryKey: QueryKey) {
  await qc.cancelQueries({ queryKey });
  await qc.invalidateQueries({ queryKey });
}

/**
 * THE entry point for refreshing the workspace inbox list. Inbox events,
 * mutations and reconnect all go through here. The list's first load is not
 * a one-off: the inbox tab is mounted lazily, so the first visit loads it
 * while notifications keep arriving.
 */
export async function refreshInboxList(qc: QueryClient, wsId: string) {
  await refreshInboxQuery(qc, inboxKeys.list(wsId));
}

/**
 * THE entry point for refreshing the cross-workspace unread summary that backs
 * the tab badge. Mutations, inbox events, issue deletion and reconnect all go
 * through here. Mirrors `onInboxSummaryInvalidate` in
 * packages/core/inbox/ws-updaters.ts.
 */
export async function refreshInboxUnreadSummary(qc: QueryClient) {
  await refreshInboxQuery(qc, inboxKeys.unreadSummary());
}

// Dropping unread rows changes the tab badge, which reads the server-side
// unread summary rather than this list. `issue:deleted` fires no `inbox:*`
// event, so nothing else would refresh it and the badge would stay above an
// empty inbox — hence the refresh lives here, not at the call site.
// Web does the same in packages/core/inbox/ws-updaters.ts (MUL-6967).
export async function dropInboxItemsByIssue(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.filter((i) => i.issue_id !== issueId),
  );
  await refreshInboxUnreadSummary(qc);
}

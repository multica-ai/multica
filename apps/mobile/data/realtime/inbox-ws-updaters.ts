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
import type { QueryClient } from "@tanstack/react-query";
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

export function dropInboxItemsByIssue(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.filter((i) => i.issue_id !== issueId),
  );
}

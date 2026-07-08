/**
 * Workspace Issues realtime — listing-level subscription. Mounted globally
 * (workspace-session-lifetime) alongside `useMyIssuesRealtime` so the
 * workspace-wide list stays fresh regardless of which tab is foregrounded.
 *
 * issue:created     — payload includes the full Issue object; prepend in
 *                     place. Avoids a refetch and keeps UI snappy when
 *                     someone else creates an issue while we're looking
 *                     at the list.
 * issue:updated     — patch in-place; client-side filter/grouping
 *                     re-derives on the next render.
 * issue:deleted     — strip from the list.
 * issue_labels:changed — handled by `patchIssueLabels` in the shared
 *                     updaters (already patches `list(wsId)` too — see
 *                     `issue-ws-updaters.ts`). No subscription needed
 *                     here because `useMyIssuesRealtime` already drives
 *                     it for every workspace surface.
 * onReconnect       — invalidate `list(wsId)` since we may have missed
 *                     a create/delete while disconnected.
 *
 * This hook is independent of `useMyIssuesRealtime` (different cache key
 * `list(wsId)` vs `myAll(wsId)`). Both are listing-level and run in
 * parallel — apps/mobile/CLAUDE.md "Mobile-owned updaters" / "list-level
 * global, per-record per-screen".
 */
import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchIssuesList,
  prependToIssuesList,
  removeFromIssuesList,
} from "./issue-ws-updaters";

export function useIssuesRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateList = () =>
        qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });

      return [
        ws.on("issue:created", (payload) => {
          prependToIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:updated", (payload) => {
          patchIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          removeFromIssuesList(qc, wsId, payload.issue_id);
        }),
        ws.onReconnect(invalidateList),
      ];
    },
    [qc],
  );
}

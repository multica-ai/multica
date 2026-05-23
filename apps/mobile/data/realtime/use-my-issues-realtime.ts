/**
 * My Issues realtime — listing-level subscriptions. Mounted globally
 * (workspace-session-lifetime) alongside useInboxRealtime so the user's
 * issue list stays fresh regardless of which tab is foregrounded.
 *
 * issue:created     — invalidate myAll(wsId). We don't try to predict
 *                     whether the new issue belongs to assigned/created/
 *                     agents scope or matches the user's current filter;
 *                     a fresh fetch is the cheapest correct answer.
 * issue:updated     — patch in-place across every cached list entry.
 *                     A status change re-buckets the SectionList on the
 *                     consumer side; we don't have to invalidate.
 * issue:deleted     — strip from every cached list entry.
 * issue_labels:changed — patch labels via the shared updater
 *                     (also handles the detail cache; harmless if
 *                     the issue isn't in any cached list).
 * onReconnect       — invalidate myAll(wsId) since we may have missed
 *                     a create/delete while disconnected.
 *
 * Inbox realtime (use-inbox-realtime.ts) handles its own keys and runs
 * in parallel; the two are independent.
 */
import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchIssueLabels,
  patchMyIssuesList,
  removeFromMyIssuesList,
} from "./issue-ws-updaters";

export function useMyIssuesRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateMyAll = () =>
        qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });

      return [
        // Server is the authority on which scopes/filters a new issue lands
        // in — we don't need to read the payload, just refetch.
        ws.on("issue:created", () => invalidateMyAll()),
        ws.on("issue:updated", (payload) => {
          patchMyIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          removeFromMyIssuesList(qc, wsId, payload.issue_id);
        }),
        ws.on("issue_labels:changed", (payload) => {
          patchIssueLabels(qc, wsId, payload.issue_id, payload.labels);
        }),
        ws.onReconnect(invalidateMyAll),
      ];
    },
    [qc],
  );
}

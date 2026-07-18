/**
 * Inbox realtime — Layer 3 of the realtime stack.
 *
 * Two subscription groups:
 *
 * 1. `inbox:*` events → invalidate the inbox query. inbox payloads are
 *    small and (apart from inbox:new) rare, so refetching is cheaper than
 *    maintaining per-event patchers. Multi-device parity: subscribing to
 *    inbox:read / inbox:archived means a read/archive on web reaches
 *    mobile within the next WS frame (web's use-realtime-sync deliberately
 *    DOESN'T subscribe to those, but mobile's stricter freshness wins for
 *    multi-device users).
 *
 * 2. `issue:*` events → patch the inbox cache directly via the dedicated
 *    updaters (inbox-ws-updaters.ts). Required because:
 *      - `issue:updated` with a new status must flip the inbox row's
 *        StatusIcon inline — otherwise the row keeps showing stale status.
 *      - `issue:deleted` must strip every inbox item pointing at that
 *        issue, otherwise tapping the orphan row 404s on issue/[id].
 *    Web does the same in `packages/core/inbox/ws-updaters.ts`.
 *
 * Reconnect: invalidate the list (we may have missed events while down;
 * no replay buffer in v1).
 */
import { useQueryClient } from "@tanstack/react-query";
import { inboxKeys } from "@/data/queries/inbox";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  dropInboxItemsByIssue,
  patchInboxIssueStatus,
} from "./inbox-ws-updaters";

export function useInboxRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () =>
        qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });

      return [
        // Inbox-domain events: refetch the small inbox list.
        ws.on("inbox:new", invalidate),
        ws.on("inbox:read", invalidate),
        ws.on("inbox:archived", invalidate),
        // Mobile has no archived view yet (web/desktop only, MUL-3736), but an
        // unarchive there restores the item to THIS list — without refetching,
        // mobile keeps showing the pre-restore list.
        ws.on("inbox:unarchived", invalidate),
        ws.on("inbox:batch-read", invalidate),
        ws.on("inbox:batch-archived", invalidate),

        // Cross-cutting: issue events that need to patch inbox state.
        ws.on("issue:updated", (payload) => {
          patchInboxIssueStatus(
            qc,
            wsId,
            payload.issue.id,
            payload.issue.status,
          );
        }),
        ws.on("issue:deleted", (payload) => {
          dropInboxItemsByIssue(qc, wsId, payload.issue_id);
        }),

        // After a reconnect we don't know what we missed during the
        // downtime — refresh from server.
        ws.onReconnect(invalidate),
      ];
    },
    [qc],
  );
}

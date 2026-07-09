/**
 * Projects realtime — listing-level subscriptions. Mounted globally
 * (workspace-session-lifetime) alongside `useMyIssuesRealtime` so the
 * project list stays fresh even if the user is on chat or an issue.
 *
 * Event coverage:
 *   - project:created → upsert into the list cache. The payload carries
 *                       the full Project; no refetch.
 *   - project:updated → patch list + detail (full replace on detail).
 *   - project:deleted → strip from list and drop detail + resources caches.
 *   - reconnect       → invalidate project list (we may have missed
 *                       create/delete events while disconnected).
 *
 * Per the patch-over-invalidate rule in apps/mobile/CLAUDE.md "Realtime →
 * Patch over invalidate (cellular-data rule)", every event with a full
 * payload patches the cache directly.
 */
import { useQueryClient } from "@tanstack/react-query";
import { projectKeys } from "@/data/queries/projects";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  clearProjectDetail,
  patchProjectDetail,
  patchProjectsList,
  removeFromProjectsList,
  upsertIntoProjectsList,
} from "./project-ws-updaters";

export function useProjectsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateList = () =>
        qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });

      return [
        ws.on("project:created", (payload) => {
          upsertIntoProjectsList(qc, wsId, payload.project);
        }),
        ws.on("project:updated", (payload) => {
          patchProjectsList(qc, wsId, payload.project);
          patchProjectDetail(qc, wsId, payload.project);
        }),
        ws.on("project:deleted", (payload) => {
          removeFromProjectsList(qc, wsId, payload.project_id);
          clearProjectDetail(qc, wsId, payload.project_id);
        }),
        ws.onReconnect(invalidateList),
      ];
    },
    [qc],
  );
}

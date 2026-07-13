/**
 * Per-project realtime subscriptions. Mounted by the project detail screen
 * with the active project id; cleans up on navigate-away.
 *
 * Filters every event by id match (`project.id === projectId` for project
 * events, `issue.project_id === projectId` for issue events) so the hook
 * only mutates the caches it owns (apps/mobile/CLAUDE.md "Realtime → Mount
 * strategy").
 *
 * Handles:
 *   - project:updated → replace detail cache (payload is full Project).
 *   - project:deleted → drop detail + resources, then fire `onDeleted` so
 *                       the screen pops back instead of stranding the user
 *                       on a 404 page.
 *   - issue:updated/created/deleted → patch the related-issues cache so
 *                       the list under the project stays in sync.
 *                       Listing-level hooks (use-my-issues-realtime) only
 *                       patch `issueKeys.myAll(wsId)`; this cache lives
 *                       under `issueKeys.list(wsId)` with a "byProject"
 *                       suffix and isn't covered by them.
 *   - reconnect       → invalidate detail + resources + related-issues
 *                       (we may have missed events while disconnected).
 *
 * `project:created` is not relevant to the per-record hook (no id match).
 */
import { useQueryClient } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { issueKeys } from "@/data/queries/issue-keys";
import { projectKeys } from "@/data/queries/projects";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  clearProjectDetail,
  patchProjectDetail,
  removeFromProjectsList,
} from "./project-ws-updaters";

export function useProjectRealtime(
  projectId: string | undefined,
  onDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!projectId) return;

      const issueListKey = [
        ...issueKeys.list(wsId),
        "byProject",
        projectId,
      ] as const;

      const invalidateThisProject = () => {
        qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
        qc.invalidateQueries({
          queryKey: projectKeys.resources(wsId, projectId),
        });
        qc.invalidateQueries({ queryKey: issueListKey });
      };

      return [
        // Project-level events
        ws.on("project:updated", (payload) => {
          if (payload.project.id !== projectId) return;
          patchProjectDetail(qc, wsId, payload.project);
        }),
        ws.on("project:deleted", (payload) => {
          if (payload.project_id !== projectId) return;
          clearProjectDetail(qc, wsId, projectId);
          removeFromProjectsList(qc, wsId, projectId);
          onDeleted?.();
        }),

        // Issue events for issues IN this project — patch the byProject
        // cache directly so the list stays fresh without a refetch.
        ws.on("issue:updated", (payload) => {
          const issue = payload.issue;
          // Status / project_id changes both matter:
          //  - if it was in this project and still is: replace in place
          //  - if it just moved INTO this project: append (server is authority on order)
          //  - if it just moved OUT: remove from this list
          const wasInList = (
            qc.getQueryData<Issue[]>(issueListKey) ?? []
          ).some((i) => i.id === issue.id);
          const nowInProject = issue.project_id === projectId;
          if (!wasInList && !nowInProject) return;
          qc.setQueryData<Issue[]>(issueListKey, (old) => {
            if (!old) return old;
            if (nowInProject) {
              return old.some((i) => i.id === issue.id)
                ? old.map((i) => (i.id === issue.id ? issue : i))
                : [...old, issue];
            }
            return old.filter((i) => i.id !== issue.id);
          });
        }),
        ws.on("issue:created", (payload) => {
          if (payload.issue.project_id !== projectId) return;
          // Server is the authority on list position — invalidate so we
          // refetch with the correct ordering rather than guessing.
          qc.invalidateQueries({ queryKey: issueListKey });
        }),
        ws.on("issue:deleted", (payload) => {
          qc.setQueryData<Issue[]>(issueListKey, (old) =>
            old ? old.filter((i) => i.id !== payload.issue_id) : old,
          );
        }),

        ws.onReconnect(invalidateThisProject),
      ];
    },
    [projectId, qc, onDeleted],
  );
}

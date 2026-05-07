import type { QueryClient } from "@tanstack/react-query";
import type { WSClient } from "@multica/core/api/ws-client";
import { getCurrentWsId } from "@multica/core/platform";
import { artifactKeys } from "@multica/cerebro-artifacts/core";

/**
 * Registers WS handlers for cerebro-only events (currently: artifact:created /
 * updated / deleted). Called once from `useRealtimeSync` in `@multica/core`.
 *
 * Server emits `{ artifact: { id, issue_id, project_id, ... } }`. Invalidate the
 * detail cache and the relevant scope's list so create/update/delete from any
 * client (including agents via MCP) reflects in the UI immediately.
 *
 * Returns a teardown function that unsubscribes all handlers.
 */
export function registerCerebroHandlers(
  ws: WSClient,
  qc: QueryClient,
): () => void {
  const invalidateArtifact = (artifact: {
    id: string;
    issue_id: string | null;
    project_id: string | null;
  }) => {
    const wsId = getCurrentWsId();
    if (!wsId) return;
    qc.invalidateQueries({ queryKey: artifactKeys.detail(wsId, artifact.id) });
    if (artifact.issue_id) {
      qc.invalidateQueries({
        queryKey: artifactKeys.byIssue(wsId, artifact.issue_id),
      });
    }
    if (artifact.project_id) {
      qc.invalidateQueries({
        queryKey: artifactKeys.byProject(wsId, artifact.project_id),
      });
    }
  };

  const unsubArtifactCreated = ws.on("artifact:created", (p) => {
    const payload = p as {
      artifact: { id: string; issue_id: string | null; project_id: string | null };
    };
    if (payload?.artifact) invalidateArtifact(payload.artifact);
  });
  const unsubArtifactUpdated = ws.on("artifact:updated", (p) => {
    const payload = p as {
      artifact: { id: string; issue_id: string | null; project_id: string | null };
    };
    if (payload?.artifact) invalidateArtifact(payload.artifact);
  });
  const unsubArtifactDeleted = ws.on("artifact:deleted", (p) => {
    const payload = p as {
      artifact: { id: string; issue_id: string | null; project_id: string | null };
    };
    if (payload?.artifact) invalidateArtifact(payload.artifact);
  });

  return () => {
    unsubArtifactCreated();
    unsubArtifactUpdated();
    unsubArtifactDeleted();
  };
}

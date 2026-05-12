import type { QueryClient } from "@tanstack/react-query";
import type { WSClient } from "@multica/core/api/ws-client";
import { getCurrentWsId } from "@multica/core/platform";
import { artifactKeys } from "@multica/cerebro-artifacts/core";
import { channelKeys } from "@multica/core/channels";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import { registerCerebroGroupHandlers } from "@multica/cerebro-groups";

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

  // chat:session_updated — JEH-799 chat-session header.
  // Title/status changes from the originating tab need to reach this device's
  // other tabs (the active list filters by status, so an archive flips the
  // session out of the active list and into the all-list view). The
  // originating tab already optimistically rewrote its caches; this handler
  // keeps everyone else in sync without a full refetch.
  const unsubChatSessionUpdated = ws.on("chat:session_updated", (p) => {
    const payload = p as {
      chat_session_id: string;
      title: string;
      status: string;
    };
    const wsId = getCurrentWsId();
    if (!wsId) return;
    const apply = (s: ChatSession): ChatSession => ({
      ...s,
      title: payload.title,
      status: payload.status as ChatSession["status"],
    });
    qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), (old) =>
      old?.map((s) => (s.id === payload.chat_session_id ? apply(s) : s)),
    );
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) => {
      if (!old) return old;
      return old.flatMap((s) => {
        if (s.id !== payload.chat_session_id) return [s];
        if (payload.status === "archived") return [];
        return [apply(s)];
      });
    });
    qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
  });

  // JEH-851 — channel archive flips the row in/out of the user's channel
  // list. The originating tab already optimistically updated its caches; this
  // handler keeps the user's other tabs/devices in sync, and re-surface
  // (server-side delete on a new inbox_item) reaches every device too.
  const invalidateChannelList = () => {
    const wsId = getCurrentWsId();
    if (!wsId) return;
    qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
  };
  const unsubChannelArchived = ws.on(
    "cerebro_channel_archived",
    invalidateChannelList,
  );
  const unsubChannelUnarchived = ws.on(
    "cerebro_channel_unarchived",
    invalidateChannelList,
  );

  // JEH-1006 — workspace groups settings list/members invalidation.
  const unsubGroups = registerCerebroGroupHandlers(ws, qc);

  return () => {
    unsubArtifactCreated();
    unsubArtifactUpdated();
    unsubArtifactDeleted();
    unsubChatSessionUpdated();
    unsubChannelArchived();
    unsubChannelUnarchived();
    unsubGroups();
  };
}

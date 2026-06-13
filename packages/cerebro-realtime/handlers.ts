import type { QueryClient } from "@tanstack/react-query";
import type { WSClient } from "@multica/core/api/ws-client";
import { getCurrentWsId } from "@multica/core/platform";
import { artifactKeys } from "@multica/cerebro-artifacts/core";
import { referenceKeys } from "@multica/cerebro-references/core";
import { channelKeys } from "@multica/core/channels";
import { chatKeys } from "@multica/core/chat/queries";
import type {
  ChatSession,
  ChatDonePayload,
  TaskCompletedPayload,
  TaskFailedPayload,
} from "@multica/core/types";
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

  // TECH-3352 — chat snooze / mark-unread changes the row's muted/unread state;
  // keep the user's other tabs/devices in sync.
  const invalidateChatSessions = () => {
    const wsId = getCurrentWsId();
    if (!wsId) return;
    qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
  };
  const unsubChatMuted = ws.on("chat:session_muted", invalidateChatSessions);
  const unsubChatUnread = ws.on("chat:session_unread", invalidateChatSessions);

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
  // TECH-3352 — per-user snooze/mark-unread changes the row's unread + muted
  // state; keep the user's other tabs/devices in sync.
  const unsubChannelStateChanged = ws.on(
    "cerebro_channel_state_changed",
    invalidateChannelList,
  );

  // JEH-838b — issue references. Server emits the reference row (with
  // issue_id) as the payload on create/update/delete. Invalidate the
  // by-issue list so a reference added/removed by any client (or an agent
  // via the API) reflects on the issue page without a refresh. Mirrors the
  // artifact invalidation pattern above.
  const invalidateReference = (p: unknown) => {
    const payload = p as { issue_id?: string } | null;
    const wsId = getCurrentWsId();
    if (!wsId || !payload?.issue_id) return;
    qc.invalidateQueries({
      queryKey: referenceKeys.byIssue(wsId, payload.issue_id),
    });
  };
  const unsubReferenceCreated = ws.on(
    "cerebro.issue_reference.created",
    invalidateReference,
  );
  const unsubReferenceUpdated = ws.on(
    "cerebro.issue_reference.updated",
    invalidateReference,
  );
  const unsubReferenceDeleted = ws.on(
    "cerebro.issue_reference.deleted",
    invalidateReference,
  );

  // FIR-2467 — live chat session cost chip. The price chip in
  // ChatSessionHeader reads chatKeys.usage(sessionId), which aggregates
  // task_usage rows persisted as tasks settle. The upstream
  // use-realtime-sync already handles chat:done / task:completed /
  // task:failed for messages + pending, but never invalidates the usage
  // query — without this the chip is stuck on the value it had when the
  // session was opened. Mirror the chat_session_id guard the upstream
  // handlers use so issue-only tasks don't trigger a chat refetch.
  const invalidateChatUsage = (sessionId: string | undefined) => {
    if (!sessionId) return;
    qc.invalidateQueries({ queryKey: chatKeys.usage(sessionId) });
  };
  const unsubChatDoneUsage = ws.on("chat:done", (p) => {
    invalidateChatUsage((p as ChatDonePayload).chat_session_id);
  });
  const unsubTaskCompletedUsage = ws.on("task:completed", (p) => {
    invalidateChatUsage((p as TaskCompletedPayload).chat_session_id);
  });
  const unsubTaskFailedUsage = ws.on("task:failed", (p) => {
    invalidateChatUsage((p as TaskFailedPayload).chat_session_id);
  });

  // JEH-1006 — workspace groups settings list/members invalidation.
  const unsubGroups = registerCerebroGroupHandlers(ws, qc);

  return () => {
    unsubArtifactCreated();
    unsubArtifactUpdated();
    unsubArtifactDeleted();
    unsubChatSessionUpdated();
    unsubChatMuted();
    unsubChatUnread();
    unsubChannelArchived();
    unsubChannelUnarchived();
    unsubChannelStateChanged();
    unsubReferenceCreated();
    unsubReferenceUpdated();
    unsubReferenceDeleted();
    unsubChatDoneUsage();
    unsubTaskCompletedUsage();
    unsubTaskFailedUsage();
    unsubGroups();
  };
}

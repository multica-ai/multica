/**
 * Chat sessions list-level realtime — Layer 3.
 *
 * Mounted globally in workspace `_layout.tsx` via `<RealtimeSubscriptions />`.
 * Keeps the chatKeys.sessions(wsId) cache fresh regardless of which tab
 * the user is on — so when they DO open Chat tab, the dropdown / sheet
 * already reflects reality (latest titles, has_unread flags, deletions).
 *
 * Terminal events also mark existing offscreen session caches stale. The
 * active screen owns its immediate patches and refetches.
 */
import { useQueryClient } from "@tanstack/react-query";
import { chatKeys } from "@/data/queries/chat";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  dropSessionFromList,
  patchSessionListAfterRename,
} from "./chat-ws-updaters";

export function useChatSessionsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateSessions = () =>
        qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      const invalidateInactiveSession = (payload: { chat_session_id?: string }) => {
        if (!payload.chat_session_id) return;
        for (const queryKey of [
          chatKeys.messages(payload.chat_session_id),
          chatKeys.pendingTask(payload.chat_session_id),
        ]) {
          void qc.invalidateQueries({ queryKey, type: "inactive", refetchType: "none" });
        }
      };
      const onTerminal = (payload: { chat_session_id?: string }) => {
        if (!payload.chat_session_id) return;
        invalidateSessions();
        invalidateInactiveSession(payload);
      };

      return [
        // chat:done flips `has_unread` server-side; refetch so the dot shows
        // even when the user isn't in the chat screen.
        ws.on("chat:done", onTerminal),
        // Cancellation may delete a queued prompt or append "Stopped.", both
        // of which change the session preview.
        ws.on("task:cancelled", onTerminal),
        // chat:done owns the list change; this is an offscreen recovery fallback.
        ws.on("task:completed", invalidateInactiveSession),
        ws.on("task:failed", onTerminal),
        // chat:session_read clears the unread flag (could be triggered from
        // web/desktop on the same account).
        ws.on("chat:session_read", invalidateSessions),
        // A channel /new creates a visible session without navigating this
        // device; refresh the list and leave the current screen untouched.
        ws.on("chat:session_created", (payload) => {
          if (payload.workspace_id === wsId) invalidateSessions();
        }),
        // chat:session_updated has no formal payload type yet — server
        // emits {chat_session_id, title?, updated_at?}. Narrow inline.
        ws.on("chat:session_updated", (p) => {
          const payload = p as {
            chat_session_id: string;
            title?: string;
            updated_at?: string;
          };
          patchSessionListAfterRename(qc, wsId, payload);
        }),
        ws.on("chat:session_deleted", (payload) => {
          dropSessionFromList(qc, wsId, payload);
        }),
        // Reconnect: we may have missed events while disconnected.
        ws.onReconnect(invalidateSessions),
      ];
    },
    [qc],
  );
}

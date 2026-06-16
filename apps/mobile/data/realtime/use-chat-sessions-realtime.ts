/**
 * Chat sessions list-level realtime — Layer 3.
 *
 * Mounted globally in workspace `_layout.tsx` via `<RealtimeSubscriptions />`.
 * Keeps the chatKeys.sessions(wsId) cache fresh regardless of which tab
 * the user is on — so when they DO open Chat tab, the dropdown / sheet
 * already reflects reality (latest titles, has_unread flags, deletions).
 *
 * Events handled here are listing-level only — per-session events
 * (chat:message, task:*) belong in `use-chat-session-realtime.ts` because
 * they target a specific session id known only inside the chat screen.
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

      return [
        // chat:done flips `has_unread` server-side; refetch so the dot shows
        // even when the user isn't in the chat screen.
        ws.on("chat:done", invalidateSessions),
        // chat:session_read clears the unread flag (could be triggered from
        // web/desktop on the same account).
        ws.on("chat:session_read", invalidateSessions),
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

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
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type {
  ChatSessionDeletedPayload,
} from "@multica/core/types";
import { useWorkspaceStore } from "@/data/workspace-store";
import { chatKeys } from "@/data/queries/chat";
import { useWSClient } from "./realtime-provider";
import {
  dropSessionFromList,
  patchSessionListAfterRename,
} from "./chat-ws-updaters";

export function useChatSessionsRealtime() {
  const ws = useWSClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  useEffect(() => {
    if (!ws || !wsId) return;

    const invalidateSessions = () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    };

    const unsubs: Array<() => void> = [
      // chat:done flips `has_unread` server-side; refetch so the dot shows
      // even when the user isn't in the chat screen.
      ws.on("chat:done", invalidateSessions),
      // chat:session_read clears the unread flag (could be triggered from
      // web/desktop on the same account).
      ws.on("chat:session_read", invalidateSessions),
      // chat:session_updated typically carries the new title — patch the
      // cached row inline.
      ws.on("chat:session_updated", (p) => {
        const payload = p as {
          chat_session_id: string;
          title?: string;
          updated_at?: string;
        };
        patchSessionListAfterRename(qc, wsId, payload);
      }),
      ws.on("chat:session_deleted", (p) => {
        dropSessionFromList(qc, wsId, p as ChatSessionDeletedPayload);
      }),
      // Reconnect: we may have missed events while disconnected.
      ws.onReconnect(invalidateSessions),
    ];

    return () => {
      for (const unsub of unsubs) unsub();
    };
  }, [ws, wsId, qc]);
}

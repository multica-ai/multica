/**
 * Per-session chat realtime — Layer 3.
 *
 * Mounted by the chat screen with the active session id; cleans up on
 * navigate-away. All handlers self-gate on `chat_session_id === sessionId`
 * so a backgrounded session (user switched sessions in the sheet but kept
 * the chat tab open) doesn't keep mutating caches it no longer owns.
 *
 * Events handled:
 *   - chat:message              → invalidate messages + pendingTask
 *   - chat:done                 → patch messages inline + clear pendingTask
 *   - task:queued / dispatch    → seed / promote pendingTask
 *   - task:cancelled            → clear pendingTask
 *   - task:completed            → no-op for messages (chat:done already
 *                                  wrote the assistant message); just
 *                                  defensive clear of pendingTask
 *   - task:failed               → clear pendingTask + invalidate messages
 *                                  (FailTask persists a failure assistant
 *                                  message that must show up)
 *   - chat:session_deleted      → fire onSessionDeleted() so the screen
 *                                  can drop the active id and unwind UI
 *   - reconnect                 → invalidate this session's messages +
 *                                  pendingTask
 */
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type {
  ChatDonePayload,
  ChatMessageEventPayload,
  ChatSessionDeletedPayload,
  TaskCancelledPayload,
  TaskCompletedPayload,
  TaskDispatchPayload,
  TaskFailedPayload,
  TaskQueuedPayload,
} from "@multica/core/types";
import { chatKeys } from "@/data/queries/chat";
import { useWSClient } from "./realtime-provider";
import {
  applyChatDoneToCache,
  clearPendingTask,
  promotePendingTaskToRunning,
  seedPendingTaskFromQueued,
} from "./chat-ws-updaters";

export function useChatSessionRealtime(
  sessionId: string | null,
  onSessionDeleted?: () => void,
) {
  const ws = useWSClient();
  const qc = useQueryClient();

  useEffect(() => {
    if (!ws || !sessionId) return;

    const isMine = (p: { chat_session_id?: string }) =>
      p.chat_session_id === sessionId;

    const invalidateMine = () => {
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
    };

    const unsubs: Array<() => void> = [
      // User-message echo from another device; we may also receive our own
      // sends echoed back, but the id-dedupe in the cache write handles
      // that. Invalidate is cheap — chat:message is rare in practice.
      ws.on("chat:message", (p) => {
        const payload = p as ChatMessageEventPayload;
        if (!isMine(payload)) return;
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      }),
      ws.on("chat:done", (p) => {
        const payload = p as ChatDonePayload;
        if (!isMine(payload)) return;
        applyChatDoneToCache(qc, payload);
      }),
      ws.on("task:queued", (p) => {
        const payload = p as TaskQueuedPayload;
        if (!isMine(payload)) return;
        seedPendingTaskFromQueued(qc, payload);
      }),
      ws.on("task:dispatch", (p) => {
        const payload = p as TaskDispatchPayload;
        if (!isMine(payload)) return;
        promotePendingTaskToRunning(qc, payload);
      }),
      ws.on("task:cancelled", (p) => {
        const payload = p as TaskCancelledPayload;
        if (!isMine(payload)) return;
        clearPendingTask(qc, sessionId);
      }),
      ws.on("task:completed", (p) => {
        const payload = p as TaskCompletedPayload;
        if (!isMine(payload)) return;
        // `chat:done` already wrote the assistant message and cleared
        // pendingTask. Defensive clear in case the two events arrive
        // out of order on a flaky network.
        clearPendingTask(qc, sessionId);
      }),
      ws.on("task:failed", (p) => {
        const payload = p as TaskFailedPayload;
        if (!isMine(payload)) return;
        // FailTask persists a destructive assistant message — surface it
        // by refetching messages and clearing the pending pill.
        clearPendingTask(qc, sessionId);
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      }),
      ws.on("chat:session_deleted", (p) => {
        const payload = p as ChatSessionDeletedPayload;
        if (!isMine(payload)) return;
        onSessionDeleted?.();
      }),
      ws.onReconnect(invalidateMine),
    ];

    return () => {
      for (const unsub of unsubs) unsub();
    };
  }, [ws, sessionId, qc, onSessionDeleted]);
}

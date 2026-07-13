/**
 * Mobile-owned WS cache patchers for the chat domain.
 *
 * Pure functions over QueryClient — no React, no WS plumbing. The
 * `use-chat-sessions-realtime` and `use-chat-session-realtime` hooks
 * translate WS events into calls into this module.
 *
 * Why mobile-owned (and not importing from web's chat ws-updaters):
 *   - Web binds its updaters to `chatKeys` from packages/core/chat/queries.ts,
 *     a different runtime instance than mobile's data/queries/chat.ts. Keys
 *     are compared structurally so it'd *appear* to work, but binding cache
 *     mutation to a foreign key factory invites silent drift the moment
 *     either side adjusts its key shape.
 *   - Mobile has a smaller cache surface (no taskMessages live timeline in
 *     v1, no per-user pending-tasks aggregate).
 *
 * Cache shapes (the design contract):
 *   - chatKeys.sessions(wsId)       → ChatSession[]
 *   - chatKeys.messages(sessionId)  → ChatMessage[]   (flat, ASC oldest→newest)
 *   - chatKeys.pendingTask(sessionId)→ ChatPendingTask (empty `{}` = no in-flight)
 */
import type { QueryClient } from "@tanstack/react-query";
import type {
  ChatDonePayload,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  ChatSessionDeletedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
  TaskDispatchPayload,
} from "@multica/core/types";
import { chatKeys } from "@/data/queries/chat";

// =====================================================
// Sessions list (ChatSession[] keyed by wsId)
// =====================================================

export function patchSessionListAfterRename(
  qc: QueryClient,
  wsId: string | null,
  payload: {
    chat_session_id: string;
    title?: string;
    updated_at?: string;
  },
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.map((s) =>
      s.id === payload.chat_session_id
        ? {
            ...s,
            title: payload.title ?? s.title,
            updated_at: payload.updated_at ?? s.updated_at,
          }
        : s,
    ),
  );
}

export function dropSessionFromList(
  qc: QueryClient,
  wsId: string | null,
  payload: ChatSessionDeletedPayload,
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.filter((s) => s.id !== payload.chat_session_id),
  );
  qc.removeQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
  qc.removeQueries({
    queryKey: chatKeys.pendingTask(payload.chat_session_id),
  });
}

export function flipSessionUnread(
  qc: QueryClient,
  wsId: string | null,
  sessionId: string,
  hasUnread: boolean,
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.map((s) =>
      s.id === sessionId ? { ...s, has_unread: hasUnread } : s,
    ),
  );
}

// =====================================================
// Messages cache (ChatMessage[] keyed by sessionId)
// =====================================================

/**
 * Apply `chat:done` to the messages cache.
 *
 * When the payload carries the freshly-persisted assistant message inline
 * (message_id + content + created_at), patch the cache directly so the
 * assistant bubble lands in the same render tick that clears pendingTask
 * — no live-timeline → final-bubble flicker.
 *
 * Older servers (pre-#2123 in web's commit history) sent only chat_session_id
 * + task_id. Detect that and fall back to invalidate; we'll refetch the
 * messages list and accept a one-frame window with no bubble.
 *
 * The inline patch never carries attachments: `ChatDonePayload` has no
 * attachments field by contract, yet an assistant reply may have images/files
 * bound to it server-side. So after the inline patch we ALWAYS invalidate the
 * messages list to refetch the authoritative rows (which include
 * `attachments`). Mirrors web's `use-realtime-sync.ts`, which invalidates
 * unconditionally after its inline patch. The patch is kept purely for the
 * instant, flicker-free bubble; the refetch is what makes attachments appear.
 */
export function applyChatDoneToCache(
  qc: QueryClient,
  payload: ChatDonePayload,
) {
  if (payload.message_id && payload.content != null && payload.created_at) {
    const assistantMsg: ChatMessage = {
      id: payload.message_id,
      chat_session_id: payload.chat_session_id,
      role: "assistant",
      content: payload.content,
      task_id: payload.task_id,
      created_at: payload.created_at,
      elapsed_ms: payload.elapsed_ms ?? null,
      // Mirror web's applyChatDoneToCache: carry the kind so a no_response turn
      // renders its notice inline; missing → "message" for older servers
      // (MUL-4351).
      message_kind: payload.message_kind ?? "message",
    };
    qc.setQueryData<ChatMessage[]>(
      chatKeys.messages(payload.chat_session_id),
      (old) => {
        if (!old) return [assistantMsg];
        // Echo guard — server may re-emit on reconnect.
        if (old.some((m) => m.id === assistantMsg.id)) return old;
        return [...old, assistantMsg];
      },
    );
  }
  // Refetch the authoritative message list so any attachments bound to the
  // assistant reply (absent from the event payload) are pulled in.
  qc.invalidateQueries({
    queryKey: chatKeys.messages(payload.chat_session_id),
  });
  // Clear in-flight pointer in the same tick so StatusPill unmounts and
  // the AssistantMessage owns the rendering.
  qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
}

// =====================================================
// Pending task (ChatPendingTask keyed by sessionId)
// =====================================================

export function seedPendingTaskFromQueued(
  qc: QueryClient,
  payload: TaskQueuedPayload,
) {
  if (!payload.chat_session_id) return;
  qc.setQueryData<ChatPendingTask>(
    chatKeys.pendingTask(payload.chat_session_id),
    (old) => ({
      ...(old ?? {}),
      task_id: payload.task_id,
      status: "queued",
    }),
  );
}

export function promotePendingTaskToRunning(
  qc: QueryClient,
  payload: TaskDispatchPayload,
) {
  if (!payload.chat_session_id) return;
  qc.setQueryData<ChatPendingTask>(
    chatKeys.pendingTask(payload.chat_session_id),
    (old) => {
      // Only upgrade if it's the task we already know about. A stale
      // dispatch event for a finished task shouldn't reanimate the pill.
      if (!old || old.task_id !== payload.task_id) return old;
      return { ...old, status: "running" };
    },
  );
}

export function clearPendingTask(
  qc: QueryClient,
  sessionId: string,
) {
  qc.setQueryData(chatKeys.pendingTask(sessionId), {});
}

// =====================================================
// Task messages (live timeline, keyed by taskId)
// =====================================================

/**
 * Append a `task:message` payload into the per-task timeline cache.
 *
 * - De-dupes on `seq` (server may re-emit on flaky network).
 * - Sorts by `seq` ASC after insert so reordered late-arriving rows still
 *   render in execution order.
 * - Creates the cache entry on first event (empty default), so the timeline
 *   is visible even before the user opens the assistant bubble that drives
 *   the lazy fetch.
 *
 * Mirrors `packages/core/realtime/use-realtime-sync.ts` ~675-689 (web's
 * single global handler). Mobile attaches per-session via
 * `use-chat-session-realtime` instead — see the WS strategy note in
 * `apps/mobile/CLAUDE.md` for why mobile prefers per-record mounts.
 */
export function appendTaskMessage(
  qc: QueryClient,
  payload: TaskMessagePayload,
) {
  qc.setQueryData<TaskMessagePayload[]>(
    chatKeys.taskMessages(payload.task_id),
    (old = []) => {
      if (old.some((m) => m.seq === payload.seq)) return old;
      return [...old, payload].sort((a, b) => a.seq - b.seq);
    },
  );
}

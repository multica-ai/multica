/**
 * Chat query keys + queryOptions factories.
 *
 * Keys:
 *   - sessions(wsId)        → ChatSession[] for the workspace dropdown / sheet
 *   - messages(sessionId)   → ChatMessage[] for the active session
 *   - pendingTask(sessionId)→ ChatPendingTask, populated when an agent task is
 *                             in flight; refreshed on terminal task events
 *
 * Same shape as web's `chatKeys` in packages/core/chat/queries.ts (mobile
 * owns its own copy per the "mirror, don't import" rule in apps/mobile/CLAUDE.md).
 *
 * WS events keep caches fresh. Messages and pending state also reconcile on
 * foreground/entry; a visible pending task polls as a fallback for lost events.
 */
import { queryOptions } from "@tanstack/react-query";
import type { ChatPendingTask } from "@multica/core/types";
import { api } from "@/data/api";
import { hasChatSendInFlight } from "@/data/chat-send-lifecycle";

export const chatKeys = {
  all: (wsId: string | null) => ["chat", wsId] as const,
  sessions: (wsId: string | null) =>
    [...chatKeys.all(wsId), "sessions"] as const,
  messages: (sessionId: string) => ["chat", "messages", sessionId] as const,
  pendingTask: (sessionId: string) =>
    ["chat", "pending-task", sessionId] as const,
  /** Per-task live execution timeline (thinking / tool_use / tool_result /
   *  text / error rows). Cache is workspace-agnostic — keyed only on
   *  `taskId` — matching web's `chatKeys.taskMessages` shape so future
   *  cross-feature consumers (issue agent cards) can share the cache.
   *  `task:message` WS events append rows in place; once the task
   *  completes the cache stays warm so the persisted assistant message
   *  can render the same trace without refetching. */
  taskMessages: (taskId: string) => ["task-messages", taskId] as const,
};

// UUID gate mirrors `packages/core/chat/queries.ts`: optimistic task ids
// (`optimistic-…`) are not real backend rows, so the query must be
// disabled until we have a server-issued UUID. Returning the cache for
// an optimistic id would 404 the API.
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isTaskMessageTaskId(
  taskId: string | null | undefined,
): taskId is string {
  return typeof taskId === "string" && UUID_PATTERN.test(taskId);
}

export const chatSessionsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: chatKeys.sessions(wsId),
    queryFn: ({ signal }) => api.listChatSessions({ signal }),
    enabled: !!wsId,
    staleTime: Infinity,
  });

export const chatMessagesOptions = (sessionId: string | null) =>
  queryOptions({
    queryKey: chatKeys.messages(sessionId ?? ""),
    queryFn: ({ signal }) => api.listChatMessages(sessionId!, { signal }),
    enabled: !!sessionId,
    staleTime: Infinity,
    refetchOnWindowFocus: () => hasChatSendInFlight(sessionId) ? false : "always",
    refetchOnMount: () => hasChatSendInFlight(sessionId) ? false : "always",
    refetchOnReconnect: () => hasChatSendInFlight(sessionId) ? false : "always",
  });

export const pendingChatTaskOptions = (sessionId: string | null) =>
  queryOptions({
    queryKey: chatKeys.pendingTask(sessionId ?? ""),
    queryFn: async ({ signal, client }) => {
      const previous = client.getQueryData<ChatPendingTask>(
        chatKeys.pendingTask(sessionId!),
      );
      const pending = await api.getPendingChatTask(sessionId!, { signal });
      // A missed terminal event must recover the final reply as well as the
      // status pill, including when a queued successor becomes the new head.
      const previousId = isTaskMessageTaskId(previous?.task_id) ? previous.task_id : undefined;
      const nextId = isTaskMessageTaskId(pending.task_id) ? pending.task_id : undefined;
      if (previous && !hasChatSendInFlight(sessionId) && (
        previousId !== nextId ||
        (previous.queued_tasks?.length && !pending.task_id && !pending.queued_tasks?.length)
      )) {
        void client.invalidateQueries({ queryKey: chatKeys.messages(sessionId!) });
      }
      return pending;
    },
    enabled: !!sessionId,
    staleTime: Infinity,
    refetchOnWindowFocus: () => hasChatSendInFlight(sessionId) ? false : "always",
    refetchOnMount: () => hasChatSendInFlight(sessionId) ? false : "always",
    refetchOnReconnect: () => hasChatSendInFlight(sessionId) ? false : "always",
    refetchInterval: (query) => !hasChatSendInFlight(sessionId) &&
      (query.state.data?.task_id || query.state.data?.queued_tasks?.length) ? 30_000 : false,
    refetchIntervalInBackground: false,
  });

export const taskMessagesOptions = (taskId: string | null | undefined) =>
  queryOptions({
    queryKey: chatKeys.taskMessages(taskId ?? ""),
    queryFn: ({ signal }) => api.listTaskMessages(taskId!, { signal }),
    enabled: isTaskMessageTaskId(taskId),
    staleTime: Infinity,
  });

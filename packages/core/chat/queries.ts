import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { TaskMessagePayload } from "../types/events";

// NOTE on workspace scoping:
// `wsId` is used only as part of queryKey for cache isolation per workspace.
// The actual workspace context comes from ApiClient's X-Workspace-Slug header,
// which is set by the URL-driven [workspaceSlug] layout. Callers must ensure
// the header is in sync with the wsId they pass here — otherwise cache writes
// will be misattributed during a workspace switch race window.

export const chatKeys = {
  all: (wsId: string) => ["chat", wsId] as const,
  /** Full sessions list (active + archived); the dropdown splits locally. */
  sessions: (wsId: string) => [...chatKeys.all(wsId), "sessions"] as const,
  session: (wsId: string, id: string) => [...chatKeys.all(wsId), "session", id] as const,
  messagesAll: () => ["chat", "messages"] as const,
  messages: (sessionId: string) => [...chatKeys.messagesAll(), sessionId] as const,
  messagesPageAll: () => ["chat", "messages-page"] as const,
  messagesPage: (sessionId: string) => [...chatKeys.messagesPageAll(), sessionId] as const,
  pendingTaskAll: () => ["chat", "pending-task"] as const,
  pendingTask: (sessionId: string) => [...chatKeys.pendingTaskAll(), sessionId] as const,
  /** Aggregate of in-flight chat tasks for the current user — FAB reads this. */
  pendingTasks: (wsId: string) => [...chatKeys.all(wsId), "pending-tasks"] as const,
  /**
   * Boolean "does the user have any in-flight chat task" — the FAB's cheap
   * running indicator. Separate cache from the detailed `pendingTasks` list so
   * the FAB (closed-window) and ChatWindow (open) can subscribe independently.
   */
  pendingTasksHasAny: (wsId: string) =>
    [...chatKeys.all(wsId), "pending-tasks", "has-any"] as const,
  /** Per-task execution messages — shared with issue agent cards. */
  taskMessagesAll: () => ["task-messages"] as const,
  taskMessages: (taskId: string) => [...chatKeys.taskMessagesAll(), taskId] as const,
};

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isTaskMessageTaskId(taskId: string | null | undefined): taskId is string {
  return typeof taskId === "string" && UUID_PATTERN.test(taskId);
}

export function chatSessionsOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.sessions(wsId),
    queryFn: () => api.listChatSessions({ status: "all" }),
    staleTime: Infinity,
  });
}

export function chatSessionOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: chatKeys.session(wsId, id),
    queryFn: () => api.getChatSession(id),
    enabled: !!id,
    staleTime: Infinity,
  });
}

export function chatMessagesOptions(sessionId: string) {
  return queryOptions({
    queryKey: chatKeys.messages(sessionId),
    queryFn: () => api.listChatMessages(sessionId),
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

export function chatMessagesPageOptions(sessionId: string, limit = 50) {
  return infiniteQueryOptions({
    queryKey: chatKeys.messagesPage(sessionId),
    queryFn: ({ pageParam }) =>
      api.listChatMessagesPage(sessionId, { before: pageParam, limit }),
    initialPageParam: null as { created_at: string; id: string } | null,
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.next_cursor ?? undefined : undefined,
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

/**
 * Pending task for a chat session — the "is something still running?" signal.
 * Refetched via WS invalidation in useRealtimeSync when chat:message / chat:done
 * / task:completed / task:failed arrive.
 */
export function pendingChatTaskOptions(sessionId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTask(sessionId),
    queryFn: () => api.getPendingChatTask(sessionId),
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

/**
 * Timeline for a single task — rendered by both the live chat view (while a
 * task is running) and AssistantMessage (for completed tasks). WS
 * `task:message` events seed this cache in real time via useRealtimeSync.
 */
export function taskMessagesOptions(taskId: string) {
  return queryOptions({
    queryKey: chatKeys.taskMessages(taskId),
    queryFn: () => api.listTaskMessages(taskId),
    enabled: isTaskMessageTaskId(taskId),
    staleTime: Infinity,
  });
}

/**
 * Merge task-message batches into one seq-ordered, seq-deduplicated list for
 * the shared `["task-messages", taskId]` cache. Existing entries win on
 * conflict, and the original array reference is preserved when nothing new
 * arrives so React Query observers don't re-render on duplicate events.
 *
 * Both the realtime `task:message` handler (a single payload) and the
 * transcript backfill (a full refetch) write this cache. Routing both through
 * one helper keeps a forced backfill from blind-replacing a seq the WebSocket
 * already delivered — and keeps a late WS event from being lost to an
 * in-flight backfill.
 */
export function mergeTaskMessagesBySeq(
  existing: readonly TaskMessagePayload[],
  incoming: readonly TaskMessagePayload[],
): TaskMessagePayload[] {
  if (incoming.length === 0) return existing as TaskMessagePayload[];
  const knownSeqs = new Set(existing.map((m) => m.seq));
  const fresh = incoming.filter((m) => !knownSeqs.has(m.seq));
  if (fresh.length === 0) return existing as TaskMessagePayload[];
  return [...existing, ...fresh].sort((a, b) => a.seq - b.seq);
}

/**
 * Aggregate of in-flight chat tasks for the current user in this workspace.
 * Drives the FAB "running" indicator while the chat window is minimised —
 * no per-session query is active then, so we need this roll-up.
 */
export function pendingChatTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTasks(wsId),
    queryFn: () => api.listPendingChatTasks(),
    staleTime: Infinity,
  });
}

/**
 * Boolean "is any chat task running for me right now" — the cheap sibling of
 * pendingChatTasksOptions. The FAB uses this (with `enabled: !isOpen`) so the
 * minimised chat button never fetches or holds the full task list; the
 * detailed list is reserved for the open ChatWindow (history + stop flows).
 * Both caches are kept in sync by the task-lifecycle WS handlers.
 */
export function hasPendingChatTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTasksHasAny(wsId),
    queryFn: () => api.hasAnyPendingChatTasks(),
    staleTime: Infinity,
  });
}

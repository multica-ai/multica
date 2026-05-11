import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

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
  messages: (sessionId: string) => ["chat", "messages", sessionId] as const,
  pendingTask: (sessionId: string) => ["chat", "pending-task", sessionId] as const,
  /** Aggregate of in-flight chat tasks for the current user — FAB reads this. */
  pendingTasks: (wsId: string) => [...chatKeys.all(wsId), "pending-tasks"] as const,
  /** Per-task execution messages — shared with issue agent cards. */
  taskMessages: (taskId: string) => ["task-messages", taskId] as const,
};

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
    enabled: !!taskId,
    staleTime: Infinity,
  });
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

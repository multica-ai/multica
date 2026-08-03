/**
 * Pull fallback for an active mobile chat task.
 *
 * WebSocket events remain the fast path. This helper only refetches the three
 * caches whose freshness is visible in an open conversation, so a missed
 * event, reconnect race, or temporarily lost subscription heals without an
 * app restart. Callers must gate it to a focused chat tab with a pending task.
 */
import type { QueryClient } from "@tanstack/react-query";
import { chatKeys, isTaskMessageTaskId } from "./chat";

export const ACTIVE_CHAT_POLL_INTERVAL_MS = 4_000;

export function shouldPollActiveChat(
  isTabFocused: boolean,
  isAppActive: boolean,
  sessionId: string | null,
  taskId: string | null | undefined,
) {
  return isTabFocused && isAppActive && !!sessionId && !!taskId;
}

/** Refetch the authoritative rows and current task pointer. The timeline is
 * added only for a server-issued task UUID; optimistic ids must never reach
 * the task-messages endpoint. */
export async function refetchActiveChat(
  qc: QueryClient,
  sessionId: string,
  taskId?: string | null,
) {
  const refetches = [
    qc.refetchQueries({ queryKey: chatKeys.messages(sessionId) }),
    qc.refetchQueries({ queryKey: chatKeys.pendingTask(sessionId) }),
  ];
  if (isTaskMessageTaskId(taskId)) {
    refetches.push(
      qc.refetchQueries({ queryKey: chatKeys.taskMessages(taskId) }),
    );
  }
  await Promise.all(refetches);
}

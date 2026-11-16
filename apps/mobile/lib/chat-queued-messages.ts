import type {
  ChatMessage,
  ChatPendingTask,
  ChatQueuedTask,
} from "@multica/core/types";

export type QueuedChatMessage = ChatQueuedTask & { content: string };

/**
 * Queued follow-ups to show under the live reply, in run order.
 *
 * `hideQueuedChatMessages` takes these messages out of the transcript, and
 * mobile has no separate queue panel, so this is the only place a queued
 * message stays visible. A row whose message the transcript still shows (a
 * send whose response has not arrived, or a cache the refetch has not
 * caught up yet) is skipped so the same text never appears twice. Rows
 * without content have nothing to preview.
 */
export function queuedChatMessages(
  pending: ChatPendingTask | undefined,
  visibleMessages: ChatMessage[],
): QueuedChatMessage[] {
  const queued = pending?.queued_tasks ?? [];
  if (queued.length === 0) return [];
  const shown = new Set(visibleMessages.map((message) => message.id));
  return queued.flatMap((task) =>
    task.content?.trim() && !(task.message_id && shown.has(task.message_id))
      ? [{ ...task, content: task.content }]
      : [],
  );
}

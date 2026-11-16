import type {
  Attachment,
  ChatMessage,
  ChatPendingTask,
} from "@multica/core/types";
import { hideQueuedChatMessages } from "@multica/core/chat/pending";

export interface QueuedChatMessage {
  task_id: string;
  content: string;
  attachments: Attachment[];
}

/**
 * Queued follow-ups to show under the live reply, in run order.
 *
 * `hideQueuedChatMessages` takes these messages out of the transcript, and
 * mobile has no separate queue panel, so this is the only place a queued
 * message stays visible. The hidden transcript row supplies attachments,
 * which the queue summary does not carry. A row whose message the
 * transcript still shows (a send whose response has not arrived, or a cache
 * the refetch has not caught up yet) is skipped so the same message never
 * appears twice. Rows with neither text nor attachments have nothing to show.
 */
export function queuedChatMessages(
  pending: ChatPendingTask | null | undefined,
  messages: ChatMessage[],
): QueuedChatMessage[] {
  const queued = pending?.queued_tasks ?? [];
  if (queued.length === 0) return [];
  const shown = new Set(
    hideQueuedChatMessages(messages, pending ?? undefined).map((m) => m.id),
  );
  const byId = new Map(messages.map((message) => [message.id, message]));
  return queued.flatMap((task) => {
    if (task.message_id && shown.has(task.message_id)) return [];
    const message = task.message_id ? byId.get(task.message_id) : undefined;
    const content = task.content ?? message?.content ?? "";
    const attachments = message?.attachments ?? [];
    if (!content.trim() && attachments.length === 0) return [];
    return [{ task_id: task.task_id, content, attachments }];
  });
}

import type { ChatMessage, ChatPendingTask, ChatQueuedTask } from "../types/chat";

const EMPTY_PENDING_TASK: ChatPendingTask = {};

function compareQueuedTasks(left: ChatQueuedTask, right: ChatQueuedTask): number {
  const leftTime = Date.parse(left.created_at);
  const rightTime = Date.parse(right.created_at);
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) {
    return leftTime - rightTime;
  }
  return left.task_id.localeCompare(right.task_id);
}

function uniqueQueue(tasks: ChatQueuedTask[]): ChatQueuedTask[] {
  const byID = new Map<string, ChatQueuedTask>();
  // Keep the first observation of a task. The send response carries the
  // authoritative created_at plus the message preview, while the later
  // task:queued event only carries an id/status. Letting that sparse event win
  // would replace visible text with the fallback label.
  for (const task of tasks) {
    if (!byID.has(task.task_id)) byID.set(task.task_id, task);
  }
  return [...byID.values()];
}

function normalizeQueue(tasks: ChatQueuedTask[]): ChatQueuedTask[] {
  return uniqueQueue(tasks).sort(compareQueuedTasks);
}

function asSummary(task: ChatPendingTask): ChatQueuedTask | undefined {
  if (!task.task_id) return undefined;
  return {
    task_id: task.task_id,
    status: task.status ?? "queued",
    created_at: task.created_at ?? "",
  };
}

export function enqueuePendingChatTask(
  current: ChatPendingTask | undefined,
  task: ChatQueuedTask,
): ChatPendingTask {
  if (!current?.task_id) {
    return { ...task, queued_tasks: [task] };
  }
  if (current.task_id === task.task_id) {
    return {
      ...current,
      status: task.status,
      created_at: current.created_at || task.created_at,
      queued_tasks: normalizeQueue([...(current.queued_tasks ?? []), task]),
    };
  }
  return {
    ...current,
    queued_tasks: normalizeQueue([...(current.queued_tasks ?? []), task]),
  };
}

export function promotePendingChatTask(
  current: ChatPendingTask | undefined,
  taskID: string,
  status: string,
  createdAt?: string,
): ChatPendingTask {
  const queue = current?.queued_tasks ?? [];
  if (current?.task_id === taskID) {
    return {
      ...current,
      status,
      queued_tasks: queue.filter((task) => task.task_id !== taskID),
    };
  }

  const promoted = queue.find((task) => task.task_id === taskID);
  // Lifecycle events are only hints. A late event for an unknown task must
  // not synthesize a new head after the authoritative pending query cleared.
  if (!current?.task_id || !promoted) return current ?? EMPTY_PENDING_TASK;
  const previousHead = asSummary(current ?? {});
  return {
    ...promoted,
    supports_queue: current.supports_queue,
    status,
    created_at: promoted.created_at || createdAt || "",
    queued_tasks: uniqueQueue([
      ...(previousHead?.status === "queued" ? [previousHead] : []),
      ...queue.filter((task) => task.task_id !== taskID),
    ]),
  };
}

export function removePendingChatTask(
  current: ChatPendingTask | undefined,
  taskID: string,
): ChatPendingTask {
  if (!current?.task_id) return EMPTY_PENDING_TASK;
  if (current.task_id !== taskID) {
    return {
      ...current,
      queued_tasks: (current.queued_tasks ?? []).filter((task) => task.task_id !== taskID),
    };
  }

  const [next, ...rest] = uniqueQueue(
    (current.queued_tasks ?? []).filter((task) => task.task_id !== taskID),
  );
  if (!next) return EMPTY_PENDING_TASK;
  return { ...next, supports_queue: current.supports_queue, queued_tasks: [next, ...rest] };
}

export function prioritizePendingChatTask(
  current: ChatPendingTask | undefined,
  taskID: string,
): ChatPendingTask {
  if (!current?.task_id) return EMPTY_PENDING_TASK;
  const queue = current.queued_tasks ?? [];
  const selected = queue.find((task) => task.task_id === taskID);
  if (!selected) return current;
  return {
    ...current,
    queued_tasks: [selected, ...queue.filter((task) => task.task_id !== taskID)],
  };
}

export function hideQueuedChatMessages(
  messages: ChatMessage[],
  pending: ChatPendingTask | undefined,
): ChatMessage[] {
  const queuedMessageIDs = new Set(
    (pending?.queued_tasks ?? []).flatMap((task) => task.message_id ? [task.message_id] : []),
  );
  if (queuedMessageIDs.size === 0) return messages;
  return messages.filter((message) => !queuedMessageIDs.has(message.id));
}

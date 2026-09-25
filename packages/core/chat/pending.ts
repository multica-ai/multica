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
  // The send response carries the message preview while task:queued is sparse.
  // Keep whichever observation is rich regardless of network arrival order.
  for (const task of tasks) {
    if (!byID.has(task.task_id) || task.message_id) byID.set(task.task_id, task);
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
  queued = Boolean(current?.task_id && current.task_id !== task.task_id),
): ChatPendingTask {
  if (!current?.task_id) {
    // A queued send can arrive before this client has loaded the real head.
    // Keep it hidden as a queue-only row until the authoritative refetch
    // fills in the head; an idle send becomes the head immediately.
    const queueSupport = current?.supports_queue === undefined
      ? {}
      : { supports_queue: current.supports_queue };
    return queued
      ? { ...queueSupport, queued_tasks: [task] }
      : {
          ...task,
          ...queueSupport,
          queued_tasks: normalizeQueue(
            (current?.queued_tasks ?? []).filter((queuedTask) =>
              queuedTask.task_id !== task.task_id
            ),
          ),
        };
  }
  if (current.task_id === task.task_id) {
    const status = current.status && current.status !== "queued"
      ? current.status
      : task.status;
    const queuedTasks = (current.queued_tasks ?? []).filter(
      (queued) => queued.task_id !== task.task_id,
    );
    return {
      ...current,
      ...task,
      status,
      created_at: current.created_at || task.created_at,
      queued_tasks: queuedTasks,
    };
  }
  if (!queued) {
    return {
      ...task,
      supports_queue: current.supports_queue,
      queued_tasks: [],
    };
  }
  return {
    ...current,
    queued_tasks: normalizeQueue([...(current.queued_tasks ?? []), task]),
  };
}

// A hold explanation outlives the hold: the daemon writes wait_reason once on
// the way in and never clears it, so the only thing keeping "held by task
// abc12345" off a task that has since started running is this gate. Any status
// other than the waiting one drops the text, whether it arrived on this event
// or was already in the cache.
function resolveWaitReason(
  status: string,
  incoming: string | undefined,
  existing: string | undefined,
): string | undefined {
  if (status !== "waiting_local_directory") return undefined;
  return incoming ?? existing;
}

export function promotePendingChatTask(
  current: ChatPendingTask | undefined,
  taskID: string,
  status: string,
  createdAt?: string,
  waitReason?: string,
): ChatPendingTask {
  const queue = current?.queued_tasks ?? [];
  if (current?.task_id === taskID) {
    return {
      ...current,
      status,
      wait_reason: resolveWaitReason(status, waitReason, current.wait_reason),
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
    wait_reason: resolveWaitReason(status, waitReason, undefined),
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
  const queueSupport = current.supports_queue === undefined
    ? {}
    : { supports_queue: current.supports_queue };
  return { ...next, ...queueSupport, queued_tasks: rest };
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
  const queuedMessageTasks = new Map(
    (pending?.queued_tasks ?? []).flatMap((task) =>
      task.message_id ? [[task.message_id, task.task_id] as const] : []
    ),
  );
  if (queuedMessageTasks.size === 0) return messages;
  return messages.filter((message) => queuedMessageTasks.get(message.id) !== message.task_id);
}

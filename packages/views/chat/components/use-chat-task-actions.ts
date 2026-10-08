"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { chatKeys } from "@multica/core/chat/queries";
import {
  prioritizePendingChatTask,
  removePendingChatTask,
} from "@multica/core/chat/pending";
import { removeChatMessageFromCaches } from "@multica/core/realtime";
import { createLogger } from "@multica/core/logger";
import type {
  Attachment,
  CancelTaskResponse,
  ChatPendingTask,
} from "@multica/core/types";
import { useT } from "../../i18n";

const apiLogger = createLogger("chat.api");

type EnqueueLocalRestore = (restore: {
  id: string;
  content: string;
  attachments?: Attachment[];
  sessionId: string;
}) => void;

type CancelOptions = {
  restoreDraftToInput: boolean;
  source: string;
  queuedAction?: "edit" | "remove";
};

export function useChatTaskActions(
  activeSessionId: string | null,
  enqueueLocalRestore: EnqueueLocalRestore,
) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const cancelChatTask = useCallback(
    async (
      taskId: string,
      sessionId: string,
      options: CancelOptions,
    ): Promise<CancelTaskResponse | null> => {
      apiLogger.info("cancelTask.start", {
        taskId,
        sessionId,
        source: options.source,
      });
      const pendingKey = chatKeys.pendingTask(sessionId);
      await qc.cancelQueries({ queryKey: pendingKey });
      const pendingSnapshot = qc.getQueryData<ChatPendingTask>(pendingKey);
      const derivedQueuedAction = !options.queuedAction &&
        (options.restoreDraftToInput &&
            pendingSnapshot?.task_id === taskId &&
            pendingSnapshot.status === "queued" &&
            pendingSnapshot.supports_queue === true);
      let queuedAction = options.queuedAction ??
        (derivedQueuedAction ? "edit" : undefined);
      qc.setQueryData<ChatPendingTask>(
        pendingKey,
        (old) => removePendingChatTask(old, taskId),
      );

      try {
        let result: CancelTaskResponse;
        try {
          result = await api.cancelTaskById(
            taskId,
            queuedAction ? { queuedAction, sessionId } : undefined,
          );
        } catch (err) {
          if (!(derivedQueuedAction && err instanceof ApiError && err.status === 409)) {
            throw err;
          }
          // The daemon claimed the task after our cached queued snapshot.
          // Preserve Stop semantics by cancelling its new active state.
          queuedAction = undefined;
          result = await api.cancelTaskById(taskId);
        }
        const restored = result.cancelled_chat_message;
        if (restored) {
          removeChatMessageFromCaches(qc, restored.chat_session_id, restored.message_id);
          if (
            restored.restore_to_input &&
            !queuedAction &&
            options.restoreDraftToInput &&
            restored.chat_session_id === sessionId
          ) {
            enqueueLocalRestore({
              id: restored.message_id,
              content: restored.content,
              attachments: restored.attachments,
              sessionId: restored.chat_session_id,
            });
          }
        }
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        apiLogger.info("cancelTask.success", {
          taskId,
          sessionId,
          restoredToInput: !!restored?.restore_to_input && options.restoreDraftToInput,
        });
        return result;
      } catch (err) {
        qc.setQueryData(pendingKey, pendingSnapshot);
        apiLogger.warn("cancelTask.error (task may have already finished)", {
          taskId,
          sessionId,
          err,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        return null;
      } finally {
        qc.invalidateQueries({ queryKey: pendingKey });
        if (queuedAction === "edit") {
          qc.invalidateQueries({ queryKey: chatKeys.draftRestores(sessionId) });
        }
        if (wsId) qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
      }
    },
    [qc, enqueueLocalRestore, wsId],
  );

  const handleEditQueuedTask = useCallback(
    async (taskId: string) => {
      if (!activeSessionId) return;
      const result = await cancelChatTask(taskId, activeSessionId, {
        restoreDraftToInput: true,
        source: "queued-message-edit",
        queuedAction: "edit",
      });
      if (!result) toast.error(t(($) => $.queue.action_failed_toast));
    },
    [activeSessionId, cancelChatTask, t],
  );

  const handleRemoveQueuedTask = useCallback(
    async (taskId: string) => {
      if (!activeSessionId) return;
      const result = await cancelChatTask(taskId, activeSessionId, {
        restoreDraftToInput: false,
        source: "queued-message-remove",
        queuedAction: "remove",
      });
      if (!result) toast.error(t(($) => $.queue.action_failed_toast));
    },
    [activeSessionId, cancelChatTask, t],
  );

  const handleClearQueuedTasks = useCallback(async () => {
    if (!activeSessionId) return;
    const pendingKey = chatKeys.pendingTask(activeSessionId);
    await qc.cancelQueries({ queryKey: pendingKey });
    try {
      await api.clearQueuedChatTasks(activeSessionId);
      const pending = qc.getQueryData<ChatPendingTask>(pendingKey);
      for (const task of pending?.queued_tasks ?? []) {
        if (task.message_id) {
          removeChatMessageFromCaches(qc, activeSessionId, task.message_id);
        }
      }
      qc.setQueryData<ChatPendingTask>(pendingKey, (old) =>
        (old?.queued_tasks ?? []).reduce<ChatPendingTask | undefined>(
          (current, task) => removePendingChatTask(current, task.task_id),
          old,
        )
      );
    } catch (err) {
      apiLogger.warn("clearQueuedTasks.error", {
        sessionId: activeSessionId,
        err,
      });
      toast.error(t(($) => $.queue.action_failed_toast));
    } finally {
      qc.invalidateQueries({ queryKey: pendingKey });
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(activeSessionId) });
      if (wsId) qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
    }
  }, [activeSessionId, qc, t, wsId]);

  const handleSendQueuedTaskNow = useCallback(
    async (taskId: string) => {
      if (!activeSessionId) return;
      const pendingKey = chatKeys.pendingTask(activeSessionId);
      await qc.cancelQueries({ queryKey: pendingKey });
      const pendingSnapshot = qc.getQueryData<ChatPendingTask>(pendingKey);
      qc.setQueryData<ChatPendingTask>(
        pendingKey,
        (old) => prioritizePendingChatTask(old, taskId),
      );
      let prioritizeSucceeded = false;
      try {
        const result = await api.prioritizeQueuedChatTask(activeSessionId, taskId);
        if (result.task_id !== taskId) throw new Error("invalid prioritize response");
        prioritizeSucceeded = true;
        if (result.active_task_id) {
          const cancelled = await cancelChatTask(result.active_task_id, activeSessionId, {
            restoreDraftToInput: true,
            source: "queued-message-send-now",
          });
          if (!cancelled) throw new Error("current task could not be stopped");
        }
      } catch (err) {
        if (!prioritizeSucceeded) qc.setQueryData(pendingKey, pendingSnapshot);
        apiLogger.warn("sendQueuedTaskNow.error", {
          taskId,
          sessionId: activeSessionId,
          err,
        });
        toast.error(
          err instanceof ApiError && err.status === 409
            ? t(($) => $.queue.steer_unavailable_toast)
            : t(($) => $.queue.action_failed_toast),
        );
      } finally {
        qc.invalidateQueries({ queryKey: pendingKey });
      }
    },
    [activeSessionId, cancelChatTask, qc, t],
  );

  return {
    cancelChatTask,
    handleEditQueuedTask,
    handleRemoveQueuedTask,
    handleClearQueuedTasks,
    handleSendQueuedTaskNow,
  };
}

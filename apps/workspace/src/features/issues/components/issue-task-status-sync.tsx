"use client";

import { useCallback } from "react";
import { useWSEvent, useWSReconnect } from "@/features/realtime";
import type {
  TaskCancelledPayload,
  TaskCompletedPayload,
  TaskDispatchPayload,
  TaskFailedPayload,
  TaskMessagePayload,
  TaskProgressPayload,
} from "@/shared/types/events";
import { useIssueTaskStore } from "@/features/issues/stores/issue-task-store";

export function IssueTaskStatusSync() {
  useWSEvent(
    "task:dispatch",
    useCallback((payload: unknown) => {
      const task = payload as TaskDispatchPayload;
      if (task.issue_id) {
        useIssueTaskStore.getState().linkTaskToIssue(task.task_id, task.issue_id);
      }
      void useIssueTaskStore.getState().refreshObservedIssues();
    }, []),
  );

  useWSEvent(
    "task:message",
    useCallback((payload: unknown) => {
      const message = payload as TaskMessagePayload;
      if (!message.issue_id) return;
      const store = useIssueTaskStore.getState();
      store.linkTaskToIssue(message.task_id, message.issue_id);

      const snapshot = store.byIssueId[message.issue_id];
      if (snapshot?.task?.id === message.task_id) {
        return;
      }

      void store.refreshIssue(message.issue_id, { force: true });
    }, []),
  );

  useWSEvent(
    "task:progress",
    useCallback((payload: unknown) => {
      const progress = payload as TaskProgressPayload;
      useIssueTaskStore.getState().setProgress(
        progress.task_id,
        progress.summary,
        progress.step,
        progress.total,
      );
    }, []),
  );

  useWSEvent(
    "task:completed",
    useCallback((payload: unknown) => {
      const task = payload as TaskCompletedPayload;
      useIssueTaskStore.getState().clearIssueTask(task.issue_id);
    }, []),
  );

  useWSEvent(
    "task:failed",
    useCallback((payload: unknown) => {
      const task = payload as TaskFailedPayload;
      useIssueTaskStore.getState().clearIssueTask(task.issue_id);
    }, []),
  );

  useWSEvent(
    "task:cancelled",
    useCallback((payload: unknown) => {
      const task = payload as TaskCancelledPayload;
      useIssueTaskStore.getState().clearIssueTask(task.issue_id);
    }, []),
  );

  useWSReconnect(
    useCallback(() => {
      void useIssueTaskStore.getState().refreshObservedIssues();
    }, []),
  );

  return null;
}
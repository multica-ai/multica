"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWSEvent, useWSReconnect } from "../realtime";
import type { AgentTask, TaskMessagePayload } from "../types";
import { issueKeys } from "./queries";

export interface LiveIssueTask {
  task: AgentTask;
  messages: TaskMessagePayload[];
}

interface TaskState {
  task: AgentTask;
  messages: TaskMessagePayload[];
}

function sortMessages(messages: TaskMessagePayload[]) {
  return [...messages].sort((a, b) => a.seq - b.seq);
}

export function useLiveIssueTasks(workspaceId: string, issueId: string) {
  const queryClient = useQueryClient();
  const [taskStates, setTaskStates] = useState<Map<string, TaskState>>(new Map());
  const [cancellingTaskIds, setCancellingTaskIds] = useState<Set<string>>(new Set());
  const seenSeqs = useRef(new Set<string>());
  const loadGeneration = useRef(0);

  const loadActiveTasks = useCallback(async () => {
    const generation = ++loadGeneration.current;
    const { tasks } = await api.getActiveTasksForIssue(issueId);
    if (generation !== loadGeneration.current) return;
    setTaskStates((prev) => {
      const next = new Map<string, TaskState>();
      for (const task of tasks) {
        const existing = prev.get(task.id);
        next.set(task.id, {
          task,
          messages: existing?.messages ?? [],
        });
      }
      return next;
    });

    await Promise.allSettled(
      tasks.map(async (task) => {
        const messages = await api.listTaskMessages(task.id);
        if (generation !== loadGeneration.current) return;
        for (const message of messages) {
          seenSeqs.current.add(`${message.task_id}:${message.seq}`);
        }
        const sorted = sortMessages(messages);
        queryClient.setQueryData(issueKeys.taskMessages(task.id), sorted);
        setTaskStates((prev) => {
          const existing = prev.get(task.id);
          if (!existing) return prev;
          const loadedSeqs = new Set(sorted.map((message) => message.seq));
          const wsOnly = existing.messages.filter((message) => !loadedSeqs.has(message.seq));
          const next = new Map(prev);
          next.set(task.id, {
            task: existing.task,
            messages: sortMessages([...sorted, ...wsOnly]),
          });
          return next;
        });
      }),
    );
  }, [issueId, queryClient, workspaceId]);

  useEffect(() => {
    void loadActiveTasks().catch((err) => {
      console.error(err);
    });
    return () => {
      loadGeneration.current += 1;
      seenSeqs.current.clear();
      setTaskStates(new Map());
      setCancellingTaskIds(new Set());
    };
  }, [loadActiveTasks]);

  useWSReconnect(() => {
    void loadActiveTasks().catch(console.error);
  });

  useWSEvent(
    "task:message",
    useCallback((payload: unknown) => {
      const message = payload as TaskMessagePayload;
      if (message.issue_id !== issueId) return;

      const key = `${message.task_id}:${message.seq}`;
      if (seenSeqs.current.has(key)) return;
      seenSeqs.current.add(key);

      queryClient.setQueryData<TaskMessagePayload[]>(
        issueKeys.taskMessages(message.task_id),
        (old = []) => {
          if (old.some((existing) => existing.seq === message.seq)) return old;
          return sortMessages([...old, message]);
        },
      );

      setTaskStates((prev) => {
        const existing = prev.get(message.task_id);
        if (!existing) return prev;
        const next = new Map(prev);
        next.set(message.task_id, {
          task: existing.task,
          messages: sortMessages([...existing.messages, message]),
        });
        return next;
      });
    }, [issueId, queryClient, workspaceId]),
  );

  useWSEvent(
    "task:dispatch",
    useCallback((payload: unknown) => {
      const dispatch = payload as { issue_id?: string };
      if (dispatch.issue_id && dispatch.issue_id !== issueId) return;
      void loadActiveTasks().catch(console.error);
    }, [issueId, loadActiveTasks]),
  );

  const handleTaskEnd = useCallback((payload: unknown) => {
    const event = payload as { task_id: string; issue_id: string };
    if (event.issue_id !== issueId) return;
    setTaskStates((prev) => {
      const next = new Map(prev);
      next.delete(event.task_id);
      return next;
    });
    setCancellingTaskIds((prev) => {
      if (!prev.has(event.task_id)) return prev;
      const next = new Set(prev);
      next.delete(event.task_id);
      return next;
    });
    void queryClient.invalidateQueries({ queryKey: issueKeys.taskRuns(issueId) });
    void queryClient.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
  }, [issueId, queryClient, workspaceId]);

  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  const cancelTask = useCallback(async (taskId: string) => {
    setCancellingTaskIds((prev) => new Set(prev).add(taskId));
    try {
      await api.cancelTask(issueId, taskId);
    } catch (err) {
      setCancellingTaskIds((prev) => {
        const next = new Set(prev);
        next.delete(taskId);
        return next;
      });
      throw err;
    }
  }, [issueId]);

  const tasks = useMemo<LiveIssueTask[]>(
    () => Array.from(taskStates.values()),
    [taskStates],
  );

  return {
    tasks,
    isActive: tasks.length > 0,
    cancellingTaskIds,
    cancelTask,
    refetch: loadActiveTasks,
  };
}

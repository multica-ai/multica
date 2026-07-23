"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  chatKeys,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  taskMessagesBackfillOptions,
  taskMessagesOptions,
} from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { useActorName } from "@multica/core/workspace/hooks";
import { ExecutionLogDialog } from "./execution-log-dialog";

export interface OpenExecutionLogOptions {
  headerSlot?: ReactNode;
  actor?: ExecutionLogActor;
}

export interface ExecutionLogActor {
  type: string;
  id: string;
}

export type OpenExecutionLog = (
  task: AgentTask,
  options?: OpenExecutionLogOptions,
) => void;

interface ExecutionLogSession {
  taskId: string;
  fallbackTask: AgentTask;
  startedActive: boolean;
  headerSlot?: ReactNode;
  actor?: ExecutionLogActor;
}

function isTerminalTask(task: AgentTask): boolean {
  return (
    task.status === "completed" ||
    task.status === "failed" ||
    task.status === "cancelled"
  );
}

/**
 * Owns one execution-log viewing session at the stable surface level.
 *
 * Rows may move between active and historical lists or disappear during a
 * query hand-off. Keeping the selected task above those rows means a status
 * transition cannot close a log the user is still reading.
 */
export function useExecutionLogSession(tasks: readonly AgentTask[]): {
  openExecutionLog: OpenExecutionLog;
  executionLogDialog: ReactNode;
} {
  const [session, setSession] = useState<ExecutionLogSession | null>(null);
  const taskById = useMemo(
    () => new Map(tasks.map((task) => [task.id, task])),
    [tasks],
  );

  const openExecutionLog = useCallback<OpenExecutionLog>((task, options) => {
    setSession({
      taskId: task.id,
      fallbackTask: task,
      startedActive: !isTerminalTask(task),
      headerSlot: options?.headerSlot,
      actor: options?.actor,
    });
  }, []);

  const closeExecutionLog = useCallback(() => setSession(null), []);
  const latestTask = session ? taskById.get(session.taskId) : undefined;
  const selectedTask = latestTask ?? session?.fallbackTask ?? null;

  // Keep the latest server-owned snapshot as a short hand-off fallback.
  // Agent activity receives active and terminal tasks from different queries,
  // so neither query is guaranteed to contain the task during the transition.
  useEffect(() => {
    if (!latestTask) return;
    setSession((current) =>
      current?.taskId === latestTask.id &&
      current.fallbackTask !== latestTask
        ? { ...current, fallbackTask: latestTask }
        : current,
    );
  }, [latestTask]);

  const isOpen = session !== null;
  useEffect(() => {
    if (!isOpen) return;
    const handleGlobalNavigate = () => closeExecutionLog();
    window.addEventListener("multica:navigate", handleGlobalNavigate);
    return () => {
      window.removeEventListener("multica:navigate", handleGlobalNavigate);
    };
  }, [closeExecutionLog, isOpen]);

  return {
    openExecutionLog,
    executionLogDialog:
      session && selectedTask ? (
        <ExecutionLogSessionHost
          session={session}
          task={selectedTask}
          onClose={closeExecutionLog}
        />
      ) : null,
  };
}

function ExecutionLogSessionHost({
  session,
  task,
  onClose,
}: {
  session: ExecutionLogSession;
  task: AgentTask;
  onClose: () => void;
}) {
  const { getActorName } = useActorName();
  const liveMessages = useLiveTaskMessages(task.id, session.startedActive);
  const actor =
    session.actor ??
    (task.agent_id ? { type: "agent", id: task.agent_id } : null);
  const agentName = actor ? getActorName(actor.type, actor.id) : "";

  return (
    <ExecutionLogDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      task={task}
      agentName={agentName}
      actorType={actor?.type}
      actorId={actor?.id}
      isLive={!isTerminalTask(task)}
      liveMessages={session.startedActive ? liveMessages : undefined}
      headerSlot={session.headerSlot}
    />
  );
}

/**
 * Read-only cache adapter for a session that began while the task was active.
 * The observer itself never fetches; one seq-merged backfill heals events that
 * arrived before the Dialog opened. Completion is handled by the paginated
 * source inside ExecutionLogDialog, so the full array is not fetched again.
 */
function useLiveTaskMessages(
  taskId: string,
  sessionStartedActive: boolean,
): TaskMessagePayload[] {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    ...taskMessagesOptions(taskId),
    enabled: false,
  });

  useEffect(() => {
    if (!sessionStartedActive || !isTaskMessageTaskId(taskId)) return;
    let cancelled = false;
    queryClient
      .fetchQuery(taskMessagesBackfillOptions(taskId))
      .then((messages) => {
        if (cancelled) return;
        queryClient.setQueryData<TaskMessagePayload[]>(
          chatKeys.taskMessages(taskId),
          (old = []) => mergeTaskMessagesBySeq(old, messages),
        );
      })
      .catch((error) => {
        console.error(error);
      });
    return () => {
      cancelled = true;
    };
  }, [queryClient, sessionStartedActive, taskId]);

  return data ?? [];
}

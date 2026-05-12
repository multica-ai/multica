"use client";

import { useCallback, useEffect, useState } from "react";
import { PauseCircle, Radio } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
import type { AgentTask } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { TaskTraceOutput } from "./task-trace-output";

interface AgentStreamSidebarProps {
  issueId: string;
}

export function AgentStreamSidebar({ issueId }: AgentStreamSidebarProps) {
  const [activeTasks, setActiveTasks] = useState<AgentTask[]>([]);
  const [recentTask, setRecentTask] = useState<AgentTask | null>(null);

  const refresh = useCallback(() => {
    Promise.all([
      api.getActiveTasksForIssue(issueId),
      api.listTasksByIssue(issueId),
    ])
      .then(([active, all]) => {
        setActiveTasks(active.tasks);
        setRecentTask(sortRecentTasks(all)[0] ?? null);
      })
      .catch(console.error);
  }, [issueId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useWSEvent("task:dispatch", (payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (!p.issue_id || p.issue_id === issueId) refresh();
  });
  useWSEvent("task:completed", refresh);
  useWSEvent("task:failed", refresh);
  useWSEvent("task:cancelled", refresh);

  const task = activeTasks[0] ?? recentTask;
  const isLive = activeTasks.length > 0;
  const runMode = taskRunMode(task);
  const [paused, setPaused] = useState(false);

  const refreshPaused = useCallback(() => {
    if (!task || !isLive) {
      setPaused(false);
      return;
    }
    api.listTaskInteractions(task.id, "pending")
      .then((items) => setPaused(items.length > 0))
      .catch(() => setPaused(false));
  }, [task, isLive]);

  useEffect(() => {
    refreshPaused();
    const interval = setInterval(refreshPaused, 3000);
    return () => clearInterval(interval);
  }, [refreshPaused]);

  useWSEvent("interaction:created", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === task?.id) refreshPaused();
  });

  useWSEvent("interaction:resolved", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === task?.id) refreshPaused();
  });

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="mb-3 flex items-center gap-2 px-1 text-sm font-medium">
        {paused ? (
          <PauseCircle className="h-3 w-3 text-indigo-500" />
        ) : (
          <Radio className={cn("h-3 w-3", isLive ? "text-success" : "text-muted-foreground")} />
        )}
        <span>Agent stream</span>
        {task && (
          <div className="ml-auto flex items-center gap-1">
            {runMode === "plan" && (
              <span className="rounded bg-info/10 px-1.5 py-0.5 text-[11px] text-info">
                plan
              </span>
            )}
            <span className={cn(
              "rounded px-1.5 py-0.5 text-[11px]",
              paused ? "bg-indigo-500/10 text-indigo-500" : isLive ? "bg-success/10 text-success" : "bg-muted text-muted-foreground",
            )}>
              {paused ? "paused" : isLive ? "live" : "recent"}
            </span>
          </div>
        )}
      </div>
      <div className="min-h-0 flex-1">
        {!task ? (
          <div className="rounded border border-dashed px-3 py-4 text-xs text-muted-foreground">
            No local agent stream yet.
          </div>
        ) : (
          <div className="h-full min-h-0 overflow-hidden rounded-md border bg-background">
            <TaskTraceOutput task={task} defaultOpen fill />
          </div>
        )}
      </div>
    </div>
  );
}

function taskRunMode(task: AgentTask | null): string {
  const value = task?.context?.run_mode;
  return typeof value === "string" ? value : "normal";
}

function sortRecentTasks(tasks: AgentTask[]): AgentTask[] {
  return [...tasks].sort((a, b) => taskTime(b) - taskTime(a));
}

function taskTime(task: AgentTask): number {
  const value = task.completed_at ?? task.started_at ?? task.dispatched_at ?? task.created_at;
  return value ? Date.parse(value) || 0 : 0;
}

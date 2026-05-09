"use client";

import { useCallback, useEffect, useState } from "react";
import { Radio } from "lucide-react";
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

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="mb-3 flex items-center gap-2 px-1 text-sm font-medium">
        <Radio className={cn("h-3 w-3", isLive ? "text-success" : "text-muted-foreground")} />
        <span>Agent stream</span>
        {task && (
          <span className={cn(
            "ml-auto rounded px-1.5 py-0.5 text-[11px]",
            isLive ? "bg-success/10 text-success" : "bg-muted text-muted-foreground",
          )}>
            {isLive ? "live" : "recent"}
          </span>
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

function sortRecentTasks(tasks: AgentTask[]): AgentTask[] {
  return [...tasks].sort((a, b) => taskTime(b) - taskTime(a));
}

function taskTime(task: AgentTask): number {
  const value = task.completed_at ?? task.started_at ?? task.dispatched_at ?? task.created_at;
  return value ? Date.parse(value) || 0 : 0;
}

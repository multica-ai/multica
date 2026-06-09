"use client";

import { useState, useEffect, useMemo } from "react";
import { AlertCircle, Clock3, ListTodo, Loader2, XCircle } from "lucide-react";
import type { Agent, AgentTask } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import { useQuery } from "@tanstack/react-query";
import { getTaskQueueBucket, getTaskQueueDisplay } from "../../config";

export function TasksTab({ agent }: { agent: Agent }) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [loading, setLoading] = useState(true);
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));

  useEffect(() => {
    setLoading(true);
    api
      .listAgentTasks(agent.id)
      .then(setTasks)
      .catch(() => setTasks([]))
      .finally(() => setLoading(false));
  }, [agent.id]);

  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 rounded-lg border px-4 py-3">
            <Skeleton className="h-4 w-4 rounded shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    );
  }

  // Sort: active tasks (running > dispatched > queued) first, then completed/failed by date
  const activeStatuses = ["running", "dispatched", "queued"];
  const sortedTasks = [...tasks].sort((a, b) => {
    const aActive = activeStatuses.indexOf(a.status);
    const bActive = activeStatuses.indexOf(b.status);
    const aIsActive = aActive !== -1;
    const bIsActive = bActive !== -1;
    if (aIsActive && !bIsActive) return -1;
    if (!aIsActive && bIsActive) return 1;
    if (aIsActive && bIsActive) return aActive - bActive;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  const issueMap = new Map(issues.map((i) => [i.id, i]));

  const queueSummary = useMemo(() => {
    const counts = { blocked: 0, queued: 0, running: 0, failed: 0 };
    const blockedItems: { id: string; title: string; blockers: number }[] = [];

    for (const task of tasks) {
      const issue = issueMap.get(task.issue_id);
      const bucket = getTaskQueueBucket(task, issue);

      if (bucket === "blocked") {
        counts.blocked += 1;
        blockedItems.push({
          id: issue?.identifier ?? task.issue_id.slice(0, 8),
          title: issue?.title ?? "Blocked task",
          blockers: issue?.blocked_by_count ?? 0,
        });
        continue;
      }

      if (bucket === "queued") {
        counts.queued += 1;
        continue;
      }

      if (bucket === "running") {
        counts.running += 1;
        continue;
      }

      if (bucket === "failed") {
        counts.failed += 1;
      }
    }

    return { counts, blockedItems: blockedItems.slice(0, 3) };
  }, [tasks, issueMap]);

  return (
    <div className="space-y-4">
      <div className="space-y-3">
        <div>
          <h3 className="text-sm font-semibold">Task Queue</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Issues assigned to this agent and their execution status.
          </p>
        </div>

        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <AlertCircle className="h-3.5 w-3.5 text-warning" />
              Blocked
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.blocked}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Clock3 className="h-3.5 w-3.5" />
              Queued
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.queued}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 text-success" />
              Running
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.running}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <XCircle className="h-3.5 w-3.5 text-destructive" />
              Failed
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.failed}</div>
          </div>
        </div>

        {(queueSummary.counts.blocked > 0 || queueSummary.counts.queued > 0) && (
          <div className="rounded-md border border-warning/20 bg-warning/5 px-3 py-3">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Dispatch status
            </div>
            <div className="mt-2 space-y-2 text-sm">
              {queueSummary.counts.blocked > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.counts.blocked} blocked</span>{" "}
                  <span className="text-muted-foreground">waiting for unresolved prerequisite issues to reach done or cancelled.</span>
                </div>
              )}
              {queueSummary.blockedItems.length > 0 && (
                <div className="space-y-1 text-xs text-muted-foreground">
                  {queueSummary.blockedItems.map((item) => (
                    <div key={`${item.id}-${item.title}`} className="flex items-start gap-2">
                      <span className="font-mono text-[11px] text-foreground">{item.id}</span>
                      <span className="truncate">{item.title}</span>
                      <span className="ml-auto shrink-0">{item.blockers} blocker{item.blockers === 1 ? "" : "s"}</span>
                    </div>
                  ))}
                </div>
              )}
              {queueSummary.counts.queued > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.counts.queued} queued</span>{" "}
                  <span className="text-muted-foreground">waiting for runtime capacity or an earlier run for the same issue to finish.</span>
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <ListTodo className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No tasks in queue</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Assign an issue to this agent to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {sortedTasks.map((task) => {
            const issue = issueMap.get(task.issue_id);
            const display = getTaskQueueDisplay(task, issue);
            const Icon = display.icon;
            const isRunning = task.status === "running";
            const isActive = task.status === "running" || task.status === "dispatched";
            const isDependencyBlocked = display.tone === "blocked";
            const isHighlighted = isActive || isDependencyBlocked;

            return (
              <div
                key={task.id}
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 ${
                  isRunning
                    ? "border-success/40 bg-success/5"
                    : task.status === "dispatched"
                      ? "border-info/40 bg-info/5"
                      : isDependencyBlocked
                        ? "border-warning/40 bg-warning/5"
                        : ""
                }`}
              >
                <Icon
                  className={`h-4 w-4 shrink-0 ${display.color} ${
                    isRunning ? "animate-spin" : ""
                  }`}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    {issue && (
                      <span className="shrink-0 text-xs font-mono text-muted-foreground">
                        {issue.identifier}
                      </span>
                    )}
                    <span className={`text-sm truncate ${isHighlighted ? "font-medium" : ""}`}>
                      {issue?.title ?? `Issue ${task.issue_id.slice(0, 8)}...`}
                    </span>
                  </div>
                  <div className={`mt-0.5 text-xs ${isDependencyBlocked ? "text-warning" : "text-muted-foreground"}`}>
                    {display.detail ?? (
                      isRunning && task.started_at
                        ? `Started ${new Date(task.started_at).toLocaleString()}`
                        : task.status === "dispatched" && task.dispatched_at
                          ? `Dispatched ${new Date(task.dispatched_at).toLocaleString()}`
                          : task.status === "completed" && task.completed_at
                            ? `Completed ${new Date(task.completed_at).toLocaleString()}`
                            : task.status === "failed" && task.completed_at
                              ? `Failed ${new Date(task.completed_at).toLocaleString()}`
                              : `Queued ${new Date(task.created_at).toLocaleString()}`
                    )}
                  </div>
                </div>
                <span className={`shrink-0 text-xs font-medium ${display.color}`}>
                  {display.label}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

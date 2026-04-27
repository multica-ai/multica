"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ListTodo,
  Loader2,
  Play,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import type { Agent, AgentTask } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useBatchUpdateIssues } from "@multica/core/issues/mutations";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { AppLink } from "../../../navigation";
import { taskStatusConfig } from "../../config";

const ACTIVE_STATUSES = ["running", "dispatched", "queued"] as const;

function isActiveTask(task: AgentTask) {
  return ACTIVE_STATUSES.includes(task.status as (typeof ACTIVE_STATUSES)[number]);
}

function canOpenTaskIssue(task: AgentTask) {
  return Boolean(task.issue_id && task.issue_identifier && task.issue_title);
}

function formatTaskHeadline(task: AgentTask) {
  if (task.trigger_source === "chat") {
    return task.trigger_excerpt?.trim() || "Responded in chat";
  }
  if (task.trigger_source === "message") {
    return task.trigger_excerpt?.trim() || "Responded to a message";
  }
  if (task.autopilot_run_id) {
    return "Autopilot-triggered execution";
  }
  if (!task.issue_id) {
    return "Task without linked issue";
  }
  return "Issue-triggered execution";
}

function formatTaskSubline(task: AgentTask) {
  if (task.issue_identifier || task.issue_title) {
    const prefix = [task.issue_identifier, task.issue_title].filter(Boolean).join(" ");
    return prefix.trim();
  }
  if (task.issue_id) {
    return "Linked issue unavailable";
  }
  if (task.chat_session_id) {
    return "Chat session";
  }
  if (task.autopilot_run_id) {
    return "Autopilot run";
  }
  return "No linked issue";
}

function formatTaskMeta(task: AgentTask) {
  if (task.status === "queued") {
    if (task.queue_blocked_reason) {
      return task.queue_blocked_reason;
    }
    if (task.queue_position) {
      return `Queue #${task.queue_position} • ${task.queue_ahead_count ?? 0} ahead`;
    }
    return `Queued ${new Date(task.created_at).toLocaleString()}`;
  }
  if (task.status === "running" && task.started_at) {
    return `Started ${new Date(task.started_at).toLocaleString()}`;
  }
  if (task.status === "dispatched" && task.dispatched_at) {
    return `Dispatched ${new Date(task.dispatched_at).toLocaleString()}`;
  }
  if (task.status === "completed" && task.completed_at) {
    return `Completed ${new Date(task.completed_at).toLocaleString()}`;
  }
  if (task.status === "failed" && task.completed_at) {
    return `Failed ${new Date(task.completed_at).toLocaleString()}`;
  }
  if (task.status === "cancelled" && task.completed_at) {
    return `Cancelled ${new Date(task.completed_at).toLocaleString()}`;
  }
  return `Queued ${new Date(task.created_at).toLocaleString()}`;
}

function TaskRowBody({ task }: { task: AgentTask }) {
  const config = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
  const Icon = config.icon;
  const isRunning = task.status === "running";
  const isActive = isActiveTask(task);

  return (
    <>
      <Icon
        className={`h-4 w-4 shrink-0 ${config.color} ${isRunning ? "animate-spin" : ""}`}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">
          {formatTaskHeadline(task)}
        </div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {formatTaskSubline(task)}
        </div>
        <div className="mt-1 text-xs text-muted-foreground">
          {formatTaskMeta(task)}
        </div>
        {task.error && task.status === "failed" && (
          <div className="mt-1 line-clamp-2 text-xs text-destructive">
            {task.error}
          </div>
        )}
      </div>
      <span
        className={`shrink-0 text-xs font-medium ${config.color} ${isActive ? "" : "opacity-80"}`}
      >
        {config.label}
      </span>
    </>
  );
}

export function TasksTab({ agent }: { agent: Agent }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [selectedTaskIds, setSelectedTaskIds] = useState<Set<string>>(new Set());
  const [cancelling, setCancelling] = useState(false);
  const batchUpdateIssues = useBatchUpdateIssues();

  const {
    data: tasks = [],
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ["workspaces", wsId, "agents", agent.id, "tasks"],
    queryFn: () => api.listAgentTasks(agent.id),
    retry: false,
  });

  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const sortedTasks = useMemo(() => {
    return [...tasks].sort((left, right) => {
      const leftActive = ACTIVE_STATUSES.indexOf(
        left.status as (typeof ACTIVE_STATUSES)[number],
      );
      const rightActive = ACTIVE_STATUSES.indexOf(
        right.status as (typeof ACTIVE_STATUSES)[number],
      );
      const leftIsActive = leftActive !== -1;
      const rightIsActive = rightActive !== -1;
      if (leftIsActive && !rightIsActive) return -1;
      if (!leftIsActive && rightIsActive) return 1;
      if (leftIsActive && rightIsActive) return leftActive - rightActive;
      return new Date(right.created_at).getTime() - new Date(left.created_at).getTime();
    });
  }, [tasks]);

  const activeTasks = sortedTasks.filter(isActiveTask);
  const historyTasks = sortedTasks.filter((task) => !isActiveTask(task));
  const selectedTasks = activeTasks.filter((task) => selectedTaskIds.has(task.id));
  const selectedIssueIds = Array.from(
    new Set(
      selectedTasks
        .map((task) => task.issue_id)
        .filter((issueId): issueId is string => !!issueId),
    ),
  );

  const toggleSelected = (taskId: string) => {
    setSelectedTaskIds((current) => {
      const next = new Set(current);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      return next;
    });
  };

  const clearSelected = () => setSelectedTaskIds(new Set());

  useEffect(() => {
    setSelectedTaskIds((current) => {
      const activeIds = new Set(activeTasks.map((task) => task.id));
      const next = new Set(Array.from(current).filter((taskId) => activeIds.has(taskId)));
      if (next.size === current.size) return current;
      return next;
    });
  }, [activeTasks]);

  const handleCancelSelected = async () => {
    if (selectedTasks.length === 0 || cancelling) return;
    setCancelling(true);
    try {
      const cancellableTasks = selectedTasks.filter((task) => !!task.issue_id);
      const results = await Promise.allSettled(
        cancellableTasks.map((task) => api.cancelTask(task.issue_id, task.id)),
      );
      const succeeded = results.filter((result) => result.status === "fulfilled").length;
      const failedIds = new Set<string>();
      results.forEach((result, index) => {
        if (result.status === "rejected") {
          const task = cancellableTasks[index];
          if (task) failedIds.add(task.id);
        }
      });
      if (succeeded > 0) {
        toast.success(`Cancelled ${succeeded} task${succeeded > 1 ? "s" : ""}`);
      }
      if (failedIds.size > 0) {
        toast.error(`${failedIds.size} task${failedIds.size > 1 ? "s" : ""} could not be cancelled`);
        setSelectedTaskIds(failedIds);
      } else {
        clearSelected();
      }
      await refetch();
    } finally {
      setCancelling(false);
    }
  };

  const handleCancelTask = async (task: AgentTask) => {
    if (!task.issue_id) return;
    try {
      await api.cancelTask(task.issue_id, task.id);
      toast.success("Task cancelled");
      setSelectedTaskIds((current) => {
        if (!current.has(task.id)) return current;
        const next = new Set(current);
        next.delete(task.id);
        return next;
      });
      await refetch();
    } catch {
      toast.error("Failed to cancel task");
    }
  };

  const handleReassign = async (agentId: string) => {
    if (selectedIssueIds.length === 0) return;
    try {
      await batchUpdateIssues.mutateAsync({
        ids: selectedIssueIds,
        updates: { assignee_type: "agent", assignee_id: agentId },
      });
      toast.success(
        `Reassigned ${selectedIssueIds.length} issue${selectedIssueIds.length > 1 ? "s" : ""}`,
      );
      clearSelected();
      await refetch();
    } catch {
      toast.error("Failed to reassign issues");
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, index) => (
          <div
            key={index}
            className="flex items-center gap-3 rounded-lg border px-4 py-3"
          >
            <Skeleton className="h-4 w-4 rounded shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-2/3" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">Execution Queue</h3>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Each row represents one execution instance, including queue status and trigger source.
        </p>
      </div>

      {selectedTasks.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2">
          <span className="text-sm font-medium">
            {selectedTasks.length} selected
          </span>
          <Button
            size="sm"
            variant="outline"
            onClick={handleCancelSelected}
            disabled={cancelling}
          >
            {cancelling ? (
              <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
            ) : (
              <XCircle className="mr-1 h-3.5 w-3.5" />
            )}
            Cancel
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  size="sm"
                  variant="outline"
                  disabled={selectedIssueIds.length === 0}
                >
                  <Play className="mr-1 h-3.5 w-3.5" />
                  Reassign Agent
                </Button>
              }
            />
            <DropdownMenuContent align="start">
              {agents
                .filter((candidate) => !candidate.archived_at && candidate.id !== agent.id)
                .map((candidate) => (
                  <DropdownMenuItem
                    key={candidate.id}
                    onClick={() => void handleReassign(candidate.id)}
                  >
                    {candidate.name}
                  </DropdownMenuItem>
                ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button size="sm" variant="ghost" onClick={clearSelected}>
            Clear
          </Button>
        </div>
      )}

      {tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <ListTodo className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No tasks in queue</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Assign an issue to this agent to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-4">
          <section className="space-y-1.5">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Active
            </div>
            {activeTasks.length === 0 ? (
              <div className="rounded-lg border border-dashed px-4 py-6 text-sm text-muted-foreground">
                No active tasks.
              </div>
            ) : (
              activeTasks.map((task) => {
                const canOpenIssue = canOpenTaskIssue(task);
                const selectable = canOpenIssue;
                const selected = selectedTaskIds.has(task.id);
                const rowClassName = cnRow(task);
                const selectionControl = (
                  <div
                    className={`shrink-0 transition-opacity ${
                      selected
                        ? "opacity-100"
                        : "opacity-0 group-hover/task:opacity-100 focus-within:opacity-100"
                    }`}
                    onClick={(event) => event.stopPropagation()}
                  >
                    <Checkbox
                      aria-label="Select task for bulk actions"
                      checked={selected}
                      disabled={!selectable}
                      onCheckedChange={() => toggleSelected(task.id)}
                    />
                  </div>
                );

                if (canOpenIssue) {
                  return (
                    <div
                      key={task.id}
                      className={`${rowClassName} group/task pr-3`}
                    >
                      {selectionControl}
                      <AppLink
                        href={paths.issueDetail(task.issue_id)}
                        className="flex min-w-0 flex-1 items-center gap-3 text-foreground no-underline hover:no-underline"
                      >
                        <TaskRowBody task={task} />
                      </AppLink>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-8 shrink-0 gap-1.5 border-destructive/30 bg-background px-2.5 font-medium text-destructive shadow-sm hover:border-destructive/50 hover:bg-destructive/10 hover:text-destructive"
                        onClick={(event) => {
                          event.preventDefault();
                          event.stopPropagation();
                          void handleCancelTask(task);
                        }}
                        aria-label="Cancel task"
                        title="Cancel task"
                      >
                        <XCircle className="h-3.5 w-3.5" />
                        Cancel
                      </Button>
                    </div>
                  );
                }

                return (
                  <div key={task.id} className={`${rowClassName} group/task`}>
                    {selectionControl}
                    <TaskRowBody task={task} />
                  </div>
                );
              })
            )}
          </section>

          <section className="space-y-1.5">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              History
            </div>
            {historyTasks.length === 0 ? (
              <div className="rounded-lg border border-dashed px-4 py-6 text-sm text-muted-foreground">
                No execution history yet.
              </div>
            ) : (
              historyTasks.map((task) => {
                const rowClassName = cnRow(task);
                const canOpenIssue = canOpenTaskIssue(task);
                const rowBody = (
                  <>
                    <div className="w-4 shrink-0" />
                    <TaskRowBody task={task} />
                  </>
                );

                if (canOpenIssue) {
                  return (
                    <div
                      key={task.id}
                      className={`${rowClassName} pr-2`}
                    >
                      <AppLink
                        href={paths.issueDetail(task.issue_id)}
                        className="flex min-w-0 flex-1 items-center gap-3 text-foreground no-underline hover:no-underline"
                      >
                        {rowBody}
                      </AppLink>
                      <span className="w-8 shrink-0" />
                    </div>
                  );
                }

                return (
                  <div key={task.id} className={rowClassName}>
                    {rowBody}
                  </div>
                );
              })
            )}
          </section>
        </div>
      )}
    </div>
  );
}

function cnRow(task: AgentTask) {
  if (task.status === "running") {
    return "flex items-center gap-3 rounded-lg border border-success/40 bg-success/5 px-4 py-3 transition-shadow hover:shadow-sm";
  }
  if (task.status === "dispatched") {
    return "flex items-center gap-3 rounded-lg border border-info/40 bg-info/5 px-4 py-3 transition-shadow hover:shadow-sm";
  }
  if (task.status === "queued") {
    return "flex items-center gap-3 rounded-lg border border-muted-foreground/20 bg-muted/20 px-4 py-3 transition-shadow hover:shadow-sm";
  }
  if (task.status === "failed") {
    return "flex items-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 transition-shadow hover:shadow-sm";
  }
  if (task.status === "completed") {
    return "flex items-center gap-3 rounded-lg border border-success/20 bg-success/5 px-4 py-3 transition-shadow hover:shadow-sm";
  }
  if (task.status === "cancelled") {
    return "flex items-center gap-3 rounded-lg border px-4 py-3 transition-shadow hover:shadow-sm";
  }
  return "flex items-center gap-3 rounded-lg border px-4 py-3 transition-shadow hover:shadow-sm";
}

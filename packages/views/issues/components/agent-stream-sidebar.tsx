"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, PauseCircle, Radio } from "lucide-react";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useWSEvent } from "@multica/core/realtime";
import type { AgentTask } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { useTimeAgo, useT } from "../../i18n";
import { stripMentionMarkdown } from "../utils/strip-mention-markdown";
import { TaskTraceOutput } from "./task-trace-output";

interface AgentStreamSidebarProps {
  issueId: string;
}

const ACTIVE_STATUSES = new Set(["queued", "dispatched", "running"]);

function taskTime(task: AgentTask): number {
  const value = task.completed_at ?? task.started_at ?? task.dispatched_at ?? task.created_at;
  return value ? Date.parse(value) || 0 : 0;
}

export function AgentStreamSidebar({ issueId }: AgentStreamSidebarProps) {
  const timeAgo = useTimeAgo();

  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });

  const activeTasks = useMemo(
    () =>
      [...tasks]
        .filter((t) => ACTIVE_STATUSES.has(t.status))
        .sort((a, b) => taskTime(b) - taskTime(a)),
    [tasks],
  );

  const recentTasks = useMemo(
    () =>
      [...tasks]
        .filter((t) => !ACTIVE_STATUSES.has(t.status))
        .sort((a, b) => taskTime(b) - taskTime(a)),
    [tasks],
  );

  const allSorted = useMemo(
    () => [...activeTasks, ...recentTasks],
    [activeTasks, recentTasks],
  );

  // Selection logic: auto-follow unless user manually picks.
  const manualSelection = useRef(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Auto-select: latest active, or latest recent.
  useEffect(() => {
    if (manualSelection.current) return;
    const candidate = activeTasks[0] ?? recentTasks[0];
    if (candidate && candidate.id !== selectedId) {
      setSelectedId(candidate.id);
    }
  }, [activeTasks, recentTasks, selectedId]);

  const selectedTask = useMemo(
    () => allSorted.find((t) => t.id === selectedId) ?? allSorted[0] ?? null,
    [allSorted, selectedId],
  );

  // Reset manual selection when selected task is gone from the list.
  useEffect(() => {
    if (manualSelection.current && selectedId && !allSorted.find((t) => t.id === selectedId)) {
      manualSelection.current = false;
      const fallback = activeTasks[0] ?? recentTasks[0];
      setSelectedId(fallback?.id ?? null);
    }
  }, [allSorted, activeTasks, recentTasks, selectedId]);

  const isLive = selectedTask != null && ACTIVE_STATUSES.has(selectedTask.status);
  const [paused, setPaused] = useState(false);

  const refreshPaused = useCallback(() => {
    if (!selectedTask || !isLive) {
      setPaused(false);
      return;
    }
    api.listTaskInteractions(selectedTask.id, "pending")
      .then((items) => setPaused(items.length > 0))
      .catch(() => setPaused(false));
  }, [selectedTask, isLive]);

  useEffect(() => {
    refreshPaused();
    const interval = setInterval(() => {
      if (document.visibilityState === "visible") refreshPaused();
    }, 30_000);
    return () => clearInterval(interval);
  }, [refreshPaused]);

  useWSEvent("interaction:created", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === selectedTask?.id) refreshPaused();
  });
  useWSEvent("interaction:resolved", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === selectedTask?.id) refreshPaused();
  });

  const [selectorOpen, setSelectorOpen] = useState(false);
  const selectorRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click.
  useEffect(() => {
    if (!selectorOpen) return;
    const handler = (e: MouseEvent) => {
      if (selectorRef.current && !selectorRef.current.contains(e.target as Node)) {
        setSelectorOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [selectorOpen]);

  const handleSelectAndClose = useCallback((id: string) => {
    manualSelection.current = true;
    setSelectedId(id);
    setSelectorOpen(false);
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      {/* Run Selector */}
      <div className="relative" ref={selectorRef}>
        <button
          type="button"
          onClick={() => setSelectorOpen((v) => !v)}
          className={cn(
            "flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left text-xs transition-colors",
            selectorOpen ? "border-border bg-accent/50" : "border-transparent hover:bg-accent/30",
          )}
        >
          {isLive ? (
            <span className="relative flex h-2 w-2 shrink-0">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-info opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-info" />
            </span>
          ) : paused ? (
            <PauseCircle className="h-3 w-3 shrink-0 text-indigo-500" />
          ) : (
            <Radio className="h-3 w-3 shrink-0 text-muted-foreground" />
          )}
          {selectedTask ? (
            <SelectorRowCompact task={selectedTask} timeAgo={timeAgo} />
          ) : (
            <span className="text-muted-foreground">No runs yet</span>
          )}
          <ChevronDown
            className={cn(
              "ml-auto h-3 w-3 shrink-0 text-muted-foreground transition-transform",
              selectorOpen && "rotate-180",
            )}
          />
        </button>
        {/* Active count badge when selector collapsed and multiple active */}
        {!selectorOpen && activeTasks.length > 1 && (
          <span className="absolute right-7 top-1/2 -translate-y-1/2 rounded-full bg-info/15 px-1.5 py-px text-[10px] font-medium tabular-nums text-info">
            {activeTasks.length}
          </span>
        )}
        {selectorOpen && (
          <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-64 overflow-y-auto rounded-md border bg-popover p-1 shadow-md">
            {activeTasks.length > 0 && (
              <>
                <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Active
                </div>
                {activeTasks.map((task) => (
                  <SelectorRowItem
                    key={task.id}
                    task={task}
                    selected={task.id === selectedId}
                    timeAgo={timeAgo}
                    onSelect={handleSelectAndClose}
                  />
                ))}
              </>
            )}
            {recentTasks.length > 0 && (
              <>
                {activeTasks.length > 0 && <div className="my-1 border-t" />}
                <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Recent
                </div>
                {recentTasks.slice(0, 20).map((task) => (
                  <SelectorRowItem
                    key={task.id}
                    task={task}
                    selected={task.id === selectedId}
                    timeAgo={timeAgo}
                    onSelect={handleSelectAndClose}
                  />
                ))}
              </>
            )}
          </div>
        )}
      </div>

      {/* Status indicator */}
      {selectedTask && (
        <div className="flex items-center gap-1.5 px-1 text-[11px]">
          {paused ? (
            <span className="rounded bg-indigo-500/10 px-1.5 py-0.5 text-indigo-500">paused</span>
          ) : isLive ? (
            <span className="rounded bg-info/10 px-1.5 py-0.5 text-info">live</span>
          ) : (
            <span className="rounded bg-muted px-1.5 py-0.5 text-muted-foreground">recent</span>
          )}
          {selectedTask.agent_id && (
            <AgentName agentId={selectedTask.agent_id} />
          )}
        </div>
      )}

      {/* Active Stream Viewer */}
      <div className="min-h-0 flex-1">
        {!selectedTask ? (
          <div className="rounded border border-dashed px-3 py-4 text-xs text-muted-foreground">
            No local agent stream yet.
          </div>
        ) : (
          <div className="h-full min-h-0 overflow-hidden rounded-md border bg-background">
            <TaskTraceOutput task={selectedTask} defaultOpen fill />
          </div>
        )}
      </div>
    </div>
  );
}

function AgentName({ agentId }: { agentId: string }) {
  const { getAgentName } = useActorName();
  const name = getAgentName(agentId);
  return <span className="truncate text-muted-foreground">{name}</span>;
}

const STATUS_TONE: Record<string, string> = {
  queued: "text-warning",
  dispatched: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

function SelectorRowCompact({ task, timeAgo }: { task: AgentTask; timeAgo: (d: string) => string }) {
  const trigger = useTriggerText(task);
  const time = activeTimeText(task, timeAgo);
  return (
    <span className="min-w-0 flex-1 overflow-hidden whitespace-nowrap text-muted-foreground">
      {trigger}
      <span className="text-muted-foreground/60"> · {time}</span>
    </span>
  );
}

function SelectorRowItem({
  task,
  selected,
  timeAgo,
  onSelect,
}: {
  task: AgentTask;
  selected: boolean;
  timeAgo: (d: string) => string;
  onSelect: (id: string) => void;
}) {
  const trigger = useTriggerText(task);
  const time = activeTimeText(task, timeAgo);
  const tone = STATUS_TONE[task.status] ?? "text-muted-foreground";
  const statusLabel = statusText(task.status);

  return (
    <button
      type="button"
      onClick={() => {
        onSelect(task.id);
        // Auto-close handled by parent via state
      }}
      className={cn(
        "flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs transition-colors",
        selected ? "bg-accent" : "hover:bg-accent/40",
      )}
    >
      {task.agent_id ? (
        <ActorAvatar actorType="agent" actorId={task.agent_id} size={18} />
      ) : (
        <span className="inline-block h-[18px] w-[18px] shrink-0 rounded-full bg-muted" />
      )}
      <span className="min-w-0 flex-1 truncate text-muted-foreground">{trigger}</span>
      <span className="shrink-0 whitespace-nowrap">
        <span className={tone}>{statusLabel}</span>
        <span className="text-muted-foreground/60"> · {time}</span>
      </span>
    </button>
  );
}

function activeTimeText(task: AgentTask, timeAgo: (d: string) => string): string {
  if (task.status === "running" && task.started_at) return timeAgo(task.started_at);
  if (task.status === "dispatched" && task.dispatched_at) return timeAgo(task.dispatched_at);
  if (task.completed_at) return timeAgo(task.completed_at);
  return timeAgo(task.created_at);
}

function statusText(status: AgentTask["status"]): string {
  switch (status) {
    case "queued": return "queued";
    case "dispatched": return "dispatched";
    case "running": return "running";
    case "completed": return "completed";
    case "failed": return "failed";
    case "cancelled": return "cancelled";
  }
}

function useTriggerText(task: AgentTask): string {
  const { t } = useT("issues");
  const { getMemberName } = useActorName();
  const isRetry = !!task.parent_task_id;
  const retryPrefix = isRetry ? "Retry" : "";

  if (task.kind === "local_cli") {
    const cli = task.cli_name || "local CLI";
    const owner = task.owner_id ? getMemberName(task.owner_id) : "";
    const cwd = task.work_dir ? basename(task.work_dir) : "";
    return [cli, owner, cwd].filter(Boolean).join(" · ");
  }

  if (task.trigger_summary) return (retryPrefix ? retryPrefix + " " : "") + stripMentionMarkdown(task.trigger_summary);
  if (isRetry) return retryPrefix || "Retry";
  if (task.autopilot_run_id) return "Autopilot";
  if (task.trigger_comment_id) return "From comment";
  return "Initial run";
}

function basename(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() || path;
}

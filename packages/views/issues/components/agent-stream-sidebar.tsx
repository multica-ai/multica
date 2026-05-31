"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { ChevronDown, MessageSquare } from "lucide-react";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useWSEvent } from "@multica/core/realtime";
import type { AgentTask } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT, useTimeAgo } from "../../i18n";
import { stripMentionMarkdown } from "../utils/strip-mention-markdown";
import { sortTaskRunsByCreatedAtDesc } from "../utils/task-runs";
import { useAgentColorMap } from "./task-agent-colors";
import { TaskTraceOutput } from "./task-trace-output";

interface AgentStreamSidebarProps {
  issueId: string;
  onHighlightComment?: (commentId: string) => void;
}

const ACTIVE_STATUSES = new Set(["queued", "dispatched", "running"]);

export function AgentStreamSidebar({ issueId, onHighlightComment }: AgentStreamSidebarProps) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();

  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });

  const chronologicalTasks = useMemo(
    () => sortTaskRunsByCreatedAtDesc(tasks),
    [tasks],
  );

  const activeTasks = useMemo(
    () => chronologicalTasks.filter((t) => ACTIVE_STATUSES.has(t.status)),
    [chronologicalTasks],
  );

  const recentTasks = useMemo(
    () => chronologicalTasks.filter((t) => !ACTIVE_STATUSES.has(t.status)),
    [chronologicalTasks],
  );

  const allSorted = useMemo(
    () => chronologicalTasks,
    [chronologicalTasks],
  );

  // Selection logic: pick a sensible default, then keep the current active run focused.
  const manualSelection = useRef(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Auto-select only when there is no current valid selection.
  useEffect(() => {
    if (manualSelection.current) return;
    if (selectedId && allSorted.some((task) => task.id === selectedId)) return;
    const candidate = activeTasks[0] ?? recentTasks[0];
    setSelectedId(candidate?.id ?? null);
  }, [activeTasks, recentTasks, allSorted, selectedId]);

  const selectedTask = useMemo(
    () => allSorted.find((t) => t.id === selectedId) ?? allSorted[0] ?? null,
    [allSorted, selectedId],
  );
  const agentColorMap = useAgentColorMap(tasks);

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
    if (!isLive) {
      setPaused(false);
      return;
    }
    refreshPaused();
    const interval = setInterval(() => {
      if (document.visibilityState === "visible") refreshPaused();
    }, 30_000);
    return () => clearInterval(interval);
  }, [refreshPaused, isLive]);

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

  const handleSelect = useCallback((id: string) => {
    manualSelection.current = true;
    setSelectedId(id);
  }, []);

  const handleSelectAndClose = useCallback((id: string) => {
    manualSelection.current = true;
    setSelectedId(id);
    setSelectorOpen(false);
  }, []);

  const persistentTasks = useMemo(
    () => {
      if (activeTasks.length > 0) return activeTasks;
      return selectedTask ? [selectedTask] : recentTasks.slice(0, 1);
    },
    [activeTasks, recentTasks, selectedTask],
  );

  const visibleTaskIds = useMemo(
    () => new Set(persistentTasks.map((task) => task.id)),
    [persistentTasks],
  );

  const hiddenRecentTasks = useMemo(
    () => recentTasks.filter((task) => !visibleTaskIds.has(task.id)).slice(0, 20),
    [recentTasks, visibleTaskIds],
  );

  const hiddenRunCount = hiddenRecentTasks.length;
  const primaryTask = persistentTasks[0] ?? null;

  const runToggleButton = (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        setSelectorOpen((v) => !v);
      }}
      className={cn(
        "flex shrink-0 items-center gap-1 rounded px-1 py-0.5 text-[11px] transition-colors",
        selectorOpen
          ? "bg-accent/70 text-foreground"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
      )}
    >
      {selectorOpen ? t(($) => $.agent_stream.collapse) : t(($) => $.agent_stream.runs)}
      <ChevronDown
        className={cn(
          "h-3 w-3 transition-transform",
          selectorOpen && "rotate-180",
        )}
      />
      {!selectorOpen && hiddenRunCount > 0 && (
        <span className="font-mono text-[10px] text-muted-foreground/70">{hiddenRunCount}</span>
      )}
    </button>
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      {/* Run selector — lightweight, no panel wrapper or title bar */}
      <div ref={selectorRef} className="shrink-0">
        {primaryTask ? (
          <>
            <PanelRunRow
              task={primaryTask}
              selected={primaryTask.id === selectedTask?.id}
              paused={primaryTask.id === selectedTask?.id ? paused : false}
              colorClass={agentColorMap?.get(primaryTask.agent_id)}
              timeAgo={timeAgo}
              onSelect={handleSelect}
              onHighlightComment={onHighlightComment}
              trailingAction={runToggleButton}
              pinned
            />
            {persistentTasks.length > 1 && (
              <div className="mt-0.5 space-y-0.5">
                {persistentTasks.slice(1).map((task) => (
                  <PanelRunRow
                    key={task.id}
                    task={task}
                    selected={task.id === selectedTask?.id}
                    paused={task.id === selectedTask?.id ? paused : false}
                    colorClass={agentColorMap?.get(task.agent_id)}
                    timeAgo={timeAgo}
                    onSelect={handleSelect}
                    onHighlightComment={onHighlightComment}
                  />
                ))}
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center justify-between gap-2 rounded-md px-2 py-2 text-xs text-muted-foreground">
            {t(($) => $.agent_stream.no_runs)}
            {runToggleButton}
          </div>
        )}

        {selectorOpen && (
          <div className="mt-1 max-h-56 overflow-y-auto pl-2">
            {hiddenRecentTasks.length > 0 && (
              <div className="space-y-0.5">
                <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70">
                  {t(($) => $.agent_stream.other_runs)}
                </div>
                {hiddenRecentTasks.map((task) => (
                  <PanelRunRow
                    key={task.id}
                    task={task}
                    selected={task.id === selectedTask?.id}
                    paused={false}
                    colorClass={agentColorMap?.get(task.agent_id)}
                    timeAgo={timeAgo}
                    onSelect={handleSelectAndClose}
                    onHighlightComment={onHighlightComment}
                  />
                ))}
              </div>
            )}

            {hiddenRunCount === 0 && (
              <div className="px-2 py-2 text-xs text-muted-foreground/70">
                {t(($) => $.agent_stream.no_other_runs)}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Stream viewer — original trace container, no extra title panel */}
      <div className="min-h-0 flex-1 overflow-hidden rounded-md border bg-background">
        {!selectedTask ? (
          <div className="flex h-full items-center justify-center px-3 py-4 text-xs text-muted-foreground">
            {t(($) => $.agent_stream.no_local_stream)}
          </div>
        ) : (
          <TaskTraceOutput key={selectedTask.id} task={selectedTask} defaultOpen fill />
        )}
      </div>
    </div>
  );
}

const STATUS_TONE: Record<string, string> = {
  queued: "text-warning",
  dispatched: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

const ROW_ACCENT_TONE: Record<string, string> = {
  queued: "border-l-warning/80",
  dispatched: "border-l-warning/80",
  running: "border-l-info/80",
  completed: "border-l-success/80",
  failed: "border-l-destructive/80",
  cancelled: "border-l-muted-foreground/50",
};

function PanelRunRow({
  task,
  selected,
  paused,
  timeAgo,
  onSelect,
  onHighlightComment,
  trailingAction,
  colorClass,
  pinned = false,
}: {
  task: AgentTask;
  selected: boolean;
  paused: boolean;
  timeAgo: (d: string) => string;
  onSelect: (id: string) => void;
  onHighlightComment?: (commentId: string) => void;
  trailingAction?: ReactNode;
  colorClass?: string;
  pinned?: boolean;
}) {
  const trigger = useTriggerText(task);
  const { t } = useT("issues");
  const time = activeTimeText(task, timeAgo);
  const tone = STATUS_TONE[task.status] ?? "text-muted-foreground";
  const statusLabel = statusText(task.status);
  const isActive = ACTIVE_STATUSES.has(task.status);
  const jumpToCommentLabel = t(($) => $.agent_stream.jump_to_comment);
  const handleCommentClick =
    task.trigger_comment_id && onHighlightComment
      ? (event: ReactMouseEvent<HTMLButtonElement>) => {
          event.stopPropagation();
          onHighlightComment(task.trigger_comment_id!);
        }
      : undefined;

  return (
    <div
      className={cn(
        "group flex w-full items-center gap-2 rounded border-l-2 px-1 py-1.5 text-left text-xs transition-colors",
        colorClass ?? (paused ? "border-l-indigo-500/80" : ROW_ACCENT_TONE[task.status] ?? "border-l-muted-foreground/50"),
        selected ? "bg-accent/45" : pinned && "bg-accent/30",
        !selected && "hover:bg-accent/40",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(task.id)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left focus-visible:outline-none"
      >
        {task.agent_id ? (
          <ActorAvatar actorType="agent" actorId={task.agent_id} size={20} />
        ) : (
          <span className="inline-block h-5 w-5 shrink-0 rounded-full bg-muted" />
        )}
        <span className={cn("min-w-0 flex-1 truncate", isActive ? "text-foreground/90" : "text-muted-foreground")}>
          {trigger}
        </span>
        <span className="shrink-0 whitespace-nowrap">
          <span className={paused ? "text-indigo-500" : tone}>{paused ? "paused" : statusLabel}</span>
          <span className="text-muted-foreground/60"> · {time}</span>
        </span>
      </button>
      {handleCommentClick && (
        <button
          type="button"
          onClick={handleCommentClick}
          aria-label={jumpToCommentLabel}
          title={jumpToCommentLabel}
          className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
        >
          <MessageSquare className="h-3.5 w-3.5" />
        </button>
      )}
      {trailingAction}
    </div>
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

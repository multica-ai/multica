"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Loader2, MessageSquarePlus, RotateCw, Search, Square, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask, TaskFailureReason } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTimeAgo } from "../../i18n";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { TranscriptButton } from "../../common/task-transcript";
import { failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { useT } from "../../i18n";
import { stripMentionMarkdown } from "../utils/strip-mention-markdown";
import { sortTaskRunsByCreatedAtAsc, sortTaskRunsByCreatedAtDesc } from "../utils/task-runs";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { RetryWithNoteDialog } from "./retry-with-note-dialog";
import { useAgentColorMap } from "./task-agent-colors";

// Mask gradient that fades the trigger-summary text into transparency at
// the right edge. Mirrors the pattern used by the desktop tab bar
// (apps/desktop/.../tab-bar.tsx) and the sidebar pin item
// (packages/views/layout/app-sidebar.tsx) — gives the row a smooth
// visual ramp toward the trailing actions instead of a hard truncate +
// ellipsis cut.
const TRIGGER_MASK_STYLE: React.CSSProperties = {
  maskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
  WebkitMaskImage:
    "linear-gradient(to right, black calc(100% - 12px), transparent)",
};

// Right-panel section that lists every agent run for this issue. Active
// runs sit at the top (always visible when present); past runs (terminal
// statuses) collapse behind a "Show past runs (N)" toggle.
//
// Replaces:
//   - the click-to-expand timeline that used to live inside AgentLiveCard
//     (sticky card stays as a header-only banner)
//   - the standalone <TaskRunHistory> below the main content
//
// Row layout — three columns, left to right:
//   1. Agent avatar (no status dot — agent availability is not the
//      story here; the row's right column carries the task status)
//   2. Trigger description (e.g. "From comment", "Autopilot", "Retry"),
//      truncated with ellipsis when narrow
//   3. Status + relative time, swapped to hover actions (cancel /
//      transcript) on hover
//
// One query (`listTasksByIssue`) drives both buckets — the back-end
// returns every status, the front-end filters into active vs past on the
// client. WS task:* events for this issue trigger an invalidate so the
// list updates without polling.

interface ExecutionLogSectionProps {
  issueId: string;
  onHighlightComment?: (commentId: string) => void;
}

export function ExecutionLogSection({ issueId, onHighlightComment }: ExecutionLogSectionProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [showPast, setShowPast] = useState(false);
  const [filter, setFilter] = useState("");

  // Cache key registered in `issueKeys.tasks` (packages/core/issues/queries.ts)
  // so the global useRealtimeSync `task:` prefix path invalidates it via
  // a `["issues", "tasks"]` prefix-match — no local WS subscriptions
  // needed, and the cache stays fresh even when this component isn't
  // mounted (e.g. user cancels from agent-side, then navigates here).
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });

  const chronologicalTasks = useMemo(
    () => sortTaskRunsByCreatedAtAsc(tasks),
    [tasks],
  );

  // Display order: newest first (descending) — matches sidebar behavior.
  const displayTasks = useMemo(
    () => sortTaskRunsByCreatedAtDesc(tasks),
    [tasks],
  );

  const agentColorMap = useAgentColorMap(chronologicalTasks);

  // Run index: sequential #1, #2, #3 across all tasks, ordered by created_at.
  // The index is an identity label — it stays stable when filtering.
  // #1 = earliest run, regardless of display order.
  const taskIndexMap = useMemo(() => {
    const map = new Map<string, number>();
    chronologicalTasks.forEach((t, i) => map.set(t.id, i + 1));
    return map;
  }, [chronologicalTasks]);

  const { getAgentName } = useActorName();

  const activeTasks = useMemo(
    () =>
      displayTasks.filter(
        (t) =>
          t.status === "queued" ||
          t.status === "dispatched" ||
          // Daemon-parked task on a busy local_directory — still active
          // (waiting on a path lock), not terminal. Surfacing it here is
          // what tells the user the agent is alive and will resume.
          t.status === "waiting_local_directory" ||
          t.status === "running",
      ),
    [displayTasks],
  );

  const pastTasks = useMemo(() => {
    return displayTasks.filter(
      (t) =>
        t.status === "completed" ||
        t.status === "failed" ||
        t.status === "cancelled",
    );
  }, [displayTasks]);

  // Filter runs by agent name or trigger summary text
  const matchesFilter = useMemo(() => {
    if (!filter) return () => true;
    const q = filter.toLowerCase();
    return (t: AgentTask) => {
      const agentName = t.agent_id ? getAgentName(t.agent_id).toLowerCase() : "";
      const summary = (t.trigger_summary ?? "").toLowerCase();
      return agentName.includes(q) || summary.includes(q);
    };
  }, [filter, getAgentName]);

  const filteredActive = useMemo(
    () => activeTasks.filter(matchesFilter),
    [activeTasks, matchesFilter],
  );
  const filteredPast = useMemo(
    () => pastTasks.filter(matchesFilter),
    [pastTasks, matchesFilter],
  );

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.execution_log.section)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
        <RunStats tasks={pastTasks} />
        {activeTasks.length > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            <span className="font-mono tabular-nums">{activeTasks.length}</span>
          </span>
        )}
      </button>
      {open && (
        <div className="space-y-0.5 pl-2">
          {/* Filter bar — only shown when there are enough runs to warrant it */}
          {tasks.length > 3 && (
            <div className="flex items-center gap-1 px-2 mb-1">
              <Search className="h-3 w-3 text-muted-foreground shrink-0" />
              <input
                type="text"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t(($) => $.execution_log.filter_placeholder)}
                className="flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
              />
              {filter && (
                <button type="button" onClick={() => setFilter("")} className="text-muted-foreground hover:text-foreground">
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
          )}

          {filteredActive.map((task) => (
            <ActiveRow
              key={task.id}
              task={task}
              issueId={issueId}
              runIndex={taskIndexMap.get(task.id)}
              colorClass={agentColorMap?.get(task.agent_id)}
              onHighlightComment={onHighlightComment}
            />
          ))}

          {filteredPast.length > 0 && (
            <>
              {filteredActive.length > 0 && (
                <div className="my-1.5 border-t border-border/60" />
              )}
              <button
                type="button"
                onClick={() => setShowPast(!showPast)}
                className="flex w-full items-center gap-1 rounded px-1 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
              >
                <ChevronRight
                  className={`!size-3 shrink-0 stroke-[2.5] transition-transform ${
                    showPast ? "rotate-90" : ""
                  }`}
                />
                {showPast
                  ? t(($) => $.execution_log.hide_past, { count: filteredPast.length })
                  : t(($) => $.execution_log.show_past, { count: filteredPast.length })}
              </button>
              {showPast && (
                <div className="mt-0.5 space-y-0.5">
                  {filteredPast.map((task) => (
                    <PastRow
                      key={task.id}
                      task={task}
                      issueId={issueId}
                      runIndex={taskIndexMap.get(task.id)}
                      colorClass={agentColorMap?.get(task.agent_id)}
                      onHighlightComment={onHighlightComment}
                    />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Trigger description ────────────────────────────────────────────────────

// Primary source: the canonical snapshot taken at task creation time
// (comment text / autopilot title). Survives source edits/deletes and
// is information-dense — far better than a structural label.
//
// Retry tasks inherit the parent's trigger_summary on the DB side (so the
// snapshot survives across attempts), but a row that just shows the
// inherited summary is indistinguishable from its parent. We prepend
// "Retry #N" when parent_task_id is set so retries are scannable as
// retries even when their summary is inherited.
//
// Fallback chain for legacy tasks created before the snapshot field
// shipped, OR for sources we don't snapshot (direct assignment / chat):
// degrade to a short structural label by trigger source. New tasks
// (post-061 migration) almost always hit the snapshot path.

// ─── Row visual config ─────────────────────────────────────────────────────

const STATUS_TONE: Record<AgentTask["status"], string> = {
  queued: "text-warning",
  dispatched: "text-warning",
  // Same tone as queued/dispatched — visually "stopped" so users see the
  // task is parked, but distinguished by the status label.
  waiting_local_directory: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

// Time anchor depends on status. Active rows want "Started 2m ago" /
// "Queued 30s ago" — what's happening now. Past rows want "5m ago" — when
// the verdict landed.
function activeTimeText(task: AgentTask, timeAgo: (dateStr: string) => string): string {
  if (task.status === "running" && task.started_at) {
    return timeAgo(task.started_at);
  }
  if (
    (task.status === "dispatched" || task.status === "waiting_local_directory") &&
    task.dispatched_at
  ) {
    return timeAgo(task.dispatched_at);
  }
  return timeAgo(task.created_at);
}

// ─── Active row ────────────────────────────────────────────────────────────

function useTriggerText(task: AgentTask): string {
  const { t } = useT("issues");
  const { getMemberName } = useActorName();
  const isRetry = !!task.parent_task_id;
  const retryPrefix = isRetry
    ? task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt_prefix, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry_prefix)
    : "";

  if (task.kind === "local_cli") {
    const cli = task.cli_name || (task.trigger_summary ? stripMentionMarkdown(task.trigger_summary) : "local CLI");
    const owner = task.owner_id ? getMemberName(task.owner_id) : "";
    const cwd = task.work_dir ? basename(task.work_dir) : "";
    return [cli, owner, cwd].filter(Boolean).join(" · ");
  }

  if (task.trigger_summary) return retryPrefix + stripMentionMarkdown(task.trigger_summary);
  if (isRetry) {
    return task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry);
  }

  // Semantic label + cleaned context summary
  if (task.autopilot_run_id) {
    const label = t(($) => $.execution_log.trigger_autopilot);
    return task.trigger_summary ? `${label} · ${task.trigger_summary}` : label;
  }
  if (task.trigger_comment_id) {
    const label = t(($) => $.execution_log.trigger_comment);
    return task.trigger_summary ? `${label} · ${task.trigger_summary}` : label;
  }
  return t(($) => $.execution_log.trigger_initial);
}

function basename(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() || path;
}

function useStatusLabel(status: AgentTask["status"]): string {
  const { t } = useT("issues");
  switch (status) {
    case "queued": return t(($) => $.execution_log.status_queued);
    case "dispatched": return t(($) => $.execution_log.status_dispatched);
    case "waiting_local_directory":
      return t(($) => $.execution_log.status_waiting_local_directory);
    case "running": return t(($) => $.execution_log.status_running);
    case "completed": return t(($) => $.execution_log.status_completed);
    case "failed": return t(($) => $.execution_log.status_failed);
    case "cancelled": return t(($) => $.execution_log.status_cancelled);
    default: return status;
  }
}

function ActiveRow({
  task,
  issueId,
  runIndex,
  colorClass,
  onHighlightComment,
}: {
  task: AgentTask;
  issueId: string;
  runIndex?: number;
  colorClass?: string;
  onHighlightComment?: (commentId: string) => void;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const [cancelling, setCancelling] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const tone = STATUS_TONE[task.status];
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);
  const time = activeTimeText(task, timeAgo);

  // Transcript only meaningful once messages exist — pure-queued tasks
  // have nothing to show yet.
  const showTranscript = task.status !== "queued";
  const showCancel = task.kind !== "local_cli";

  const handleCancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.cancel_failed));
      setCancelling(false);
    }
  };

  const handleTriggerClick =
    task.trigger_comment_id && onHighlightComment
      ? () => onHighlightComment(task.trigger_comment_id!)
      : undefined;

  const requestCancel = () => {
    if (cancelling) return;
    setConfirmOpen(true);
  };

  return (
    <RowShell task={task} runIndex={runIndex} colorClass={colorClass}>
      <TriggerText text={trigger} fullText={task.trigger_summary} onClick={handleTriggerClick} />
      {/* Status + time always visible — actions append on hover, never
          replace. Same pattern as desktop tab bar / sidebar pins. */}
      <span className="shrink-0 whitespace-nowrap text-xs">
        <span className={tone}>{label}</span>
        <span className="text-muted-foreground"> · {time}</span>
      </span>
      <RowActions>
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName=""
            isLive
            title={t(($) => $.execution_log.transcript_tooltip)}
          />
        )}
        {showCancel && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={requestCancel}
                  disabled={cancelling}
                  aria-label={t(($) => $.execution_log.cancel_task_aria)}
                />
              }
              className="flex items-center justify-center rounded p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {cancelling ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Square className="h-3.5 w-3.5" />
              )}
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.execution_log.cancel_task_tooltip)}</TooltipContent>
          </Tooltip>
        )}
      </RowActions>
      {showCancel && (
        <TerminateTaskConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          onConfirm={() => void handleCancel()}
          showRunningNote={task.status === "running" || task.status === "dispatched"}
        />
      )}
    </RowShell>
  );
}

// ─── Past row ──────────────────────────────────────────────────────────────

function PastRow({
  task,
  issueId,
  runIndex,
  colorClass,
  onHighlightComment,
}: {
  task: AgentTask;
  issueId: string;
  runIndex?: number;
  colorClass?: string;
  onHighlightComment?: (commentId: string) => void;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const [retrying, setRetrying] = useState(false);
  const [retryWithNoteOpen, setRetryWithNoteOpen] = useState(false);
  const tone = STATUS_TONE[task.status];
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);
  const time = task.completed_at ? timeAgo(task.completed_at) : "—";
  const failureLabel =
    task.status === "failed" && task.failure_reason
      ? failureReasonLabel[task.failure_reason as TaskFailureReason]
      : null;
  const exitCodeText =
    task.kind === "local_cli" && task.exit_code != null
      ? `exit ${task.exit_code}`
      : null;

  const handleTriggerClick =
    task.trigger_comment_id && onHighlightComment
      ? () => onHighlightComment(task.trigger_comment_id!)
      : undefined;
  const canRetry = task.kind !== "local_cli" && !!task.agent_id;

  const retryTask = async (retryInstruction?: string) => {
    if (retrying || !canRetry) return;
    setRetrying(true);
    try {
      await api.rerunIssue(issueId, task.id, retryInstruction);
      await qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.retry_failed));
      throw e;
    } finally {
      setRetrying(false);
    }
  };

  return (
    <RowShell task={task} runIndex={runIndex} colorClass={colorClass}>
      <TriggerText text={trigger} fullText={task.trigger_summary} onClick={handleTriggerClick} />
      <span className="shrink-0 whitespace-nowrap text-xs">
        <span className={tone}>{exitCodeText ?? failureLabel ?? label}</span>
        <span className="text-muted-foreground"> · {time}</span>
      </span>
      <RowActions>
        {canRetry && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => void retryTask()}
                  disabled={retrying}
                  aria-label={t(($) => $.execution_log.retry_task_aria)}
                />
              }
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              {retrying ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RotateCw className="h-3.5 w-3.5" />
              )}
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.execution_log.retry_task_tooltip)}</TooltipContent>
          </Tooltip>
        )}
        {canRetry && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => setRetryWithNoteOpen(true)}
                  disabled={retrying}
                  aria-label={t(($) => $.retry_with_note.action)}
                />
              }
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <MessageSquarePlus className="h-3.5 w-3.5" />
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.retry_with_note.action)}</TooltipContent>
          </Tooltip>
        )}
        <TranscriptButton task={task} agentName="" title={t(($) => $.execution_log.transcript_tooltip)} />
      </RowActions>
      <RetryWithNoteDialog
        open={retryWithNoteOpen}
        pending={retrying}
        onOpenChange={setRetryWithNoteOpen}
        onSubmit={(note) => retryTask(note)}
      />
    </RowShell>
  );
}

// ─── Shared row chrome ─────────────────────────────────────────────────────

function RowShell({
  task,
  runIndex,
  colorClass,
  children,
}: {
  task: AgentTask;
  runIndex?: number;
  colorClass?: string;
  children: React.ReactNode;
}) {
  // `relative` so the absolute-positioned RowActions slot anchors to this
  // row instead of an outer container.
  return (
    <div
      className={`group relative flex items-center gap-2 rounded px-1 py-1.5 transition-colors hover:bg-accent/40${
        colorClass ? ` border-l-2 ${colorClass}` : ""
      }`}
    >
      {runIndex != null && (
        <span className="shrink-0 w-5 text-right text-[10px] font-mono tabular-nums text-muted-foreground/60">
          #{runIndex}
        </span>
      )}
      {task.agent_id ? (
        <ActorAvatar
          actorType="agent"
          actorId={task.agent_id}
          size={20}
          enableHoverCard
        />
      ) : (
        <span className="inline-block h-5 w-5 shrink-0 rounded-full bg-muted" />
      )}
      {children}
    </div>
  );
}

// Trigger description with a mask-gradient right edge — text fades into
// transparency in the trailing 12px for the same reason desktop tab /
// sidebar pin do it: avoids a hard truncate cut against neighbouring
// content. When the display text is truncated and fullText is provided,
// a Tooltip reveals the complete trigger_summary on hover.
function TriggerText({ text, fullText, onClick }: { text: string; fullText?: string; onClick?: () => void }) {
  const Tag = onClick ? "button" : "span";
  const content = (
    <Tag
      type={onClick ? "button" : undefined}
      onClick={onClick}
      className={`min-w-0 flex-1 overflow-hidden whitespace-nowrap text-xs text-muted-foreground text-left${
        onClick ? " hover:underline cursor-pointer" : ""
      }`}
      style={TRIGGER_MASK_STYLE}
    >
      {text}
    </Tag>
  );

  if (!fullText || fullText === text) return content;

  return (
    <Tooltip>
      <TooltipTrigger
        render={content}
        className="min-w-0 flex-1"
      />
      <TooltipContent className="max-w-xs whitespace-pre-wrap text-xs">{fullText}</TooltipContent>
    </Tooltip>
  );
}

// Hover-only action slot — absolute-positioned over the row's right edge.
// Status + time stay anchored in the layout; on hover the action buttons
// fade in on top of them with a left-fading gradient backdrop, so the
// status copy is gracefully covered (not hard-clipped) and the row
// content never reflows. Mirrors the "actions sticky over content" idiom
// used by GitHub PR rows, Linear issue rows, etc.
function RowActions({ children }: { children: React.ReactNode }) {
  return (
    <div
      className={[
        "pointer-events-none absolute inset-y-0 right-1 flex items-center gap-0.5 pl-6 opacity-0 transition-opacity",
        // The gradient backdrop blends the row's hover background (accent/40)
        // from the right and fades to transparent on the left, so the
        // status text underneath is dimmed gracefully rather than cut.
        "bg-gradient-to-l from-accent/95 via-accent/80 to-transparent",
        "group-hover:pointer-events-auto group-hover:opacity-100",
        "group-focus-within:pointer-events-auto group-focus-within:opacity-100",
      ].join(" ")}
    >
      {children}
    </div>
  );
}

// ─── Run statistics overview ───────────────────────────────────────────────
// Compact ✓/✗/⊘ counters shown in the section title bar, left of the
// active-pulse indicator. Only counts terminal statuses.
function RunStats({ tasks }: { tasks: AgentTask[] }) {
  const counts = useMemo(() => {
    let completed = 0;
    let failed = 0;
    let cancelled = 0;
    for (const t of tasks) {
      if (t.status === "completed") completed++;
      else if (t.status === "failed") failed++;
      else if (t.status === "cancelled") cancelled++;
    }
    return { completed, failed, cancelled, total: completed + failed + cancelled };
  }, [tasks]);

  if (counts.total === 0) return null;

  return (
    <span className="ml-auto flex items-center gap-1.5 text-xs tabular-nums text-muted-foreground">
      {counts.completed > 0 && (
        // eslint-disable-next-line i18next/no-literal-string
        <span className="text-success">{counts.completed}✓</span>
      )}
      {counts.failed > 0 && (
        // eslint-disable-next-line i18next/no-literal-string
        <span className="text-destructive">{counts.failed}✗</span>
      )}
      {/* eslint-disable-next-line i18next/no-literal-string */}
      {counts.cancelled > 0 && <span>{counts.cancelled}⊘</span>}
    </span>
  );
}

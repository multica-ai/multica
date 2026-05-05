"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, Square } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { timeAgo } from "@multica/core/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { TranscriptButton } from "../../common/task-transcript";
import { useT } from "@multica/i18n/react";

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
}

// Past-runs sort priority: failed first (needs attention), then
// cancelled (procedural noise), then completed (the boring 'done'
// case sinks to the bottom). Within each group, newest first.
const PAST_STATUS_RANK: Record<string, number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
};

export function ExecutionLogSection({ issueId }: ExecutionLogSectionProps) {
  const t = useT("issues");
  const [open, setOpen] = useState(true);
  const [showPast, setShowPast] = useState(false);

  // Cache key registered in `issueKeys.tasks` (packages/core/issues/queries.ts)
  // so the global useRealtimeSync `task:` prefix path invalidates it via
  // a `["issues", "tasks"]` prefix-match — no local WS subscriptions
  // needed, and the cache stays fresh even when this component isn't
  // mounted (e.g. user cancels from agent-side, then navigates here).
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const activeTasks = useMemo(
    () =>
      tasks.filter(
        (t) =>
          t.status === "queued" ||
          t.status === "dispatched" ||
          t.status === "running",
      ),
    [tasks],
  );

  const pastTasks = useMemo(() => {
    const past = tasks.filter(
      (t) =>
        t.status === "completed" ||
        t.status === "failed" ||
        t.status === "cancelled",
    );
    // Stable sort: failed first, cancelled second, completed last.
    // Within group: newest completed_at first (fall back to created_at
    // for malformed rows missing completed_at).
    return [...past].sort((a, b) => {
      const rankDiff =
        (PAST_STATUS_RANK[a.status] ?? 99) -
        (PAST_STATUS_RANK[b.status] ?? 99);
      if (rankDiff !== 0) return rankDiff;
      const at = a.completed_at ?? a.created_at;
      const bt = b.completed_at ?? b.created_at;
      return new Date(bt).getTime() - new Date(at).getTime();
    });
  }, [tasks]);

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
        onClick={() => setOpen(!open)}
      >
        {t("execution_log")}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
        {activeTasks.length > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            <span className="font-mono tabular-nums">{activeTasks.length}</span>
          </span>
        )}
      </button>
      {open && (
        <div className="space-y-0.5 pl-2">
          {activeTasks.map((task) => (
            <ActiveRow key={task.id} task={task} issueId={issueId} />
          ))}

          {pastTasks.length > 0 && (
            <>
              {activeTasks.length > 0 && (
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
                {showPast ? t("execution_hide") : t("execution_show")} {t("execution_past_runs")} ({pastTasks.length})
              </button>
              {showPast && (
                <div className="mt-0.5 space-y-0.5">
                  {pastTasks.map((task) => (
                    <PastRow key={task.id} task={task} />
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
function buildTriggerText(task: AgentTask, t: ReturnType<typeof useT>): string {
  const isRetry = !!task.parent_task_id;
  const retryPrefix = isRetry
    ? task.attempt && task.attempt > 1
      ? `${t("execution_retry")} #${task.attempt} · `
      : `${t("execution_retry")} · `
    : "";

  if (task.trigger_summary) return retryPrefix + task.trigger_summary;
  if (isRetry) {
    return task.attempt && task.attempt > 1 ? `${t("execution_retry")} #${task.attempt}` : t("execution_retry");
  }
  if (task.autopilot_run_id) return t("execution_autopilot_run");
  if (task.trigger_comment_id) return t("execution_comment_trigger");
  return t("execution_initial_run");
}

// ─── Row visual config ─────────────────────────────────────────────────────

function getStatusVisual(status: AgentTask["status"], t: ReturnType<typeof useT>): { label: string; tone: string } {
  const map: Record<AgentTask["status"], { label: string; tone: string }> = {
    queued: { label: t("execution_queued"), tone: "text-warning" },
    dispatched: { label: t("execution_starting"), tone: "text-warning" },
    running: { label: t("execution_working"), tone: "text-info" },
    completed: { label: t("execution_completed"), tone: "text-success" },
    failed: { label: t("execution_failed"), tone: "text-destructive" },
    cancelled: { label: t("execution_cancelled"), tone: "text-muted-foreground" },
  };
  return map[status];
}

// Time anchor depends on status. Active rows want "Started 2m ago" /
// "Queued 30s ago" — what's happening now. Past rows want "5m ago" — when
// the verdict landed.
function activeTimeText(task: AgentTask): string {
  if (task.status === "running" && task.started_at) {
    return timeAgo(task.started_at);
  }
  if (task.status === "dispatched" && task.dispatched_at) {
    return timeAgo(task.dispatched_at);
  }
  return timeAgo(task.created_at);
}

// ─── Active row ────────────────────────────────────────────────────────────

function ActiveRow({ task, issueId }: { task: AgentTask; issueId: string }) {
  const t = useT("issues");
  const [cancelling, setCancelling] = useState(false);
  const cfg = getStatusVisual(task.status, t);
  const trigger = buildTriggerText(task, t);
  const time = activeTimeText(task);

  // Transcript only meaningful once messages exist — pure-queued tasks
  // have nothing to show yet.
  const showTranscript = task.status !== "queued";

  const handleCancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_cancel_task"));
      setCancelling(false);
    }
  };

  return (
    <RowShell task={task}>
      <TriggerText text={trigger} />
      {/* Status + time always visible — actions append on hover, never
          replace. Same pattern as desktop tab bar / sidebar pins. */}
      <span className="shrink-0 whitespace-nowrap text-xs">
        <span className={cfg.tone}>{cfg.label}</span>
        <span className="text-muted-foreground"> · {time}</span>
      </span>
      <RowActions>
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName=""
            isLive
            title={t("execution_view_transcript")}
          />
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={handleCancel}
                disabled={cancelling}
                aria-label={t("execution_cancel_task")}
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
          <TooltipContent>{t("execution_cancel_task")}</TooltipContent>
        </Tooltip>
      </RowActions>
    </RowShell>
  );
}

// ─── Past row ──────────────────────────────────────────────────────────────

function PastRow({ task }: { task: AgentTask }) {
  const t = useT("issues");
  const cfg = getStatusVisual(task.status, t);
  const trigger = buildTriggerText(task, t);
  const time = task.completed_at ? timeAgo(task.completed_at) : "—";
  const failureLabel =
    task.status === "failed" && task.failure_reason
      ? t(`task_failure_${task.failure_reason}`)
      : null;

  return (
    <RowShell task={task}>
      <TriggerText text={trigger} />
      <span className="shrink-0 whitespace-nowrap text-xs">
        <span className={cfg.tone}>{failureLabel ?? cfg.label}</span>
        <span className="text-muted-foreground"> · {time}</span>
      </span>
      <RowActions>
        <TranscriptButton task={task} agentName="" title={t("execution_view_transcript")} />
      </RowActions>
    </RowShell>
  );
}

// ─── Shared row chrome ─────────────────────────────────────────────────────

function RowShell({
  task,
  children,
}: {
  task: AgentTask;
  children: React.ReactNode;
}) {
  // `relative` so the absolute-positioned RowActions slot anchors to this
  // row instead of an outer container.
  return (
    <div className="group relative flex items-center gap-2 rounded px-1 py-1.5 transition-colors hover:bg-accent/40">
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
// content.
function TriggerText({ text }: { text: string }) {
  return (
    <span
      className="min-w-0 flex-1 overflow-hidden whitespace-nowrap text-xs text-muted-foreground"
      style={TRIGGER_MASK_STYLE}
    >
      {text}
    </span>
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

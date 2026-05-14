"use client";

// CEREBRO-PATCH(agent-live-card-cerebro): cerebro modification of upstream file

import { useState, useEffect, useCallback, useRef } from "react";
import { Bot, ChevronRight, Loader2, Brain, AlertCircle, Clock, CheckCircle2, XCircle, Square, Maximize2, Play, GitFork, Plus } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { TaskMessagePayload, TaskCompletedPayload, TaskFailedPayload, TaskCancelledPayload } from "@multica/core/types/events";
import type { AgentTask, WorkSession } from "@multica/core/types/agent";
import { cn } from "@multica/ui/lib/utils";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { toast } from "sonner";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  TranscriptButton,
  buildTimeline,
  type TimelineItem,
} from "../../common/task-transcript";
import { AgentTranscriptDialog } from "../../common/task-transcript/agent-transcript-dialog";
import { redactSecrets } from "../../common/task-transcript/redact";
// CEREBRO-PATCH(agent-live-card-cerebro): import getToolSummary from cerebro-chat after Phase 6 relocation
import { getToolSummary } from "@multica/cerebro-chat/views";
import { useT } from "../../i18n";

// Local helper — formats a start/end timestamp pair as "Ns" / "Nm Ns".
function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

// AgentLiveCard renders a sticky banner at the top of the issue's main
// column for every active task. Each banner shows "agent X is working",
// elapsed time, tool count, and Cancel/Transcript actions.
//
// The full timeline (live execution log) used to live inside an
// expandable area on this card. It now lives in the right panel via
// ExecutionLogSection — this card is just a header-style anchor that
// answers "is anyone working on this issue right now?" at a glance.
//
// We still maintain per-task TimelineItem[] state here so the live
// TranscriptButton on the sticky banner can open the dialog with live
// items already attached (the dialog stays in sync via WS as messages
// arrive). The right-panel rows use the lazy mode of TranscriptButton
// instead — a one-shot fetch when opened. Both modes coexist.

function formatElapsed(startedAt: string): string {
  const elapsed = Date.now() - new Date(startedAt).getTime();
  const seconds = Math.floor(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

interface TaskState {
  task: AgentTask;
  items: TimelineItem[];
}

interface AgentLiveCardProps {
  issueId: string;
}

export function AgentLiveCard({ issueId }: AgentLiveCardProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [taskStates, setTaskStates] = useState<Map<string, TaskState>>(new Map());
  const seenSeqs = useRef(new Set<string>());
  const hydratedTaskIds = useRef(new Set<string>());
  const mountedRef = useRef(true);
  // Monotonic counter — each reconcile() call captures its issued seq and
  // only applies its response if it's still the latest issued. This stops
  // a slow getActiveTasksForIssue response from clobbering newer truth
  // (e.g. a stale "task is active" payload re-adding a banner that a
  // newer "tasks: []" response just cleared).
  const reconcileSeq = useRef(0);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  // Reconcile local state to server truth. Replaces taskStates with the
  // server's active set: tasks no longer active are dropped (this is what
  // self-heals a stale "is working" banner when a task:completed/failed/
  // cancelled event was lost during a WS reconnect window), and tasks
  // still active keep their accumulated TimelineItems so the live
  // TranscriptButton doesn't lose history. New tasks get a one-shot
  // listTaskMessages hydration to backfill any messages that landed
  // before the WS subscription saw them.
  const reconcile = useCallback(() => {
    const mySeq = ++reconcileSeq.current;
    api.getActiveTasksForIssue(issueId).then(({ tasks }) => {
      if (!mountedRef.current) return;
      // A newer reconcile was issued after this one — drop this response
      // unconditionally and let the latest request win, regardless of
      // resolution order. Without this guard, a slow A then a fast B can
      // resolve in B-then-A order and A re-adds tasks B already cleared.
      if (mySeq !== reconcileSeq.current) return;
      const activeIds = new Set(tasks.map((t) => t.id));

      setTaskStates((prev) => {
        const next = new Map<string, TaskState>();
        for (const task of tasks) {
          const existing = prev.get(task.id);
          next.set(task.id, existing
            ? { task, items: existing.items }
            : { task, items: [] });
        }
        return next;
      });

      // Drop bookkeeping for tasks that vanished, so a future re-dispatch
      // of the same id (very rare, but possible) re-hydrates cleanly.
      for (const key of Array.from(seenSeqs.current)) {
        const taskId = key.slice(0, key.indexOf(":"));
        if (!activeIds.has(taskId)) seenSeqs.current.delete(key);
      }
      for (const id of Array.from(hydratedTaskIds.current)) {
        if (!activeIds.has(id)) hydratedTaskIds.current.delete(id);
      }

      // Hydrate messages for tasks we haven't fetched yet. Per-task guard
      // prevents duplicate fetches when reconcile fires repeatedly (mount
      // + reconnect + queued/dispatch can stack within a single tick).
      for (const task of tasks) {
        if (hydratedTaskIds.current.has(task.id)) continue;
        hydratedTaskIds.current.add(task.id);
        api.listTaskMessages(task.id).then((msgs) => {
          if (!mountedRef.current) return;
          const timeline = buildTimeline(msgs);
          for (const m of msgs) seenSeqs.current.add(`${m.task_id}:${m.seq}`);
          setTaskStates((prev) => {
            const next = new Map(prev);
            const existing = next.get(task.id);
            if (!existing) return prev;
            const loadedSeqs = new Set(timeline.map((i) => i.seq));
            const wsOnly = existing.items.filter((i) => !loadedSeqs.has(i.seq));
            const merged = [...timeline, ...wsOnly].sort((a, b) => a.seq - b.seq);
            next.set(task.id, { task: existing.task, items: merged });
            return next;
          });
        }).catch((e) => {
          hydratedTaskIds.current.delete(task.id);
          console.error(e);
        });
      }
    }).catch(console.error);
  }, [issueId]);

  // Initial fetch on mount / issueId change.
  useEffect(() => {
    reconcile();
  }, [reconcile]);

  // WS reconnect — anything that happened while we were offline (most
  // notably task:completed / task:failed / task:cancelled) won't replay,
  // so re-pull the truth and let reconcile drop any stale banners.
  useWSReconnect(reconcile);

  // Real-time messages — route by task_id and dedupe by seq.
  useWSEvent(
    "task:message",
    useCallback((payload: unknown) => {
      const msg = payload as TaskMessagePayload;
      if (msg.issue_id !== issueId) return;
      const key = `${msg.task_id}:${msg.seq}`;
      if (seenSeqs.current.has(key)) return;
      seenSeqs.current.add(key);

      const item: TimelineItem = {
        seq: msg.seq,
        type: msg.type,
        tool: msg.tool,
        content: msg.content,
        input: msg.input,
        output: msg.output,
      };

      setTaskStates((prev) => {
        const next = new Map(prev);
        const existing = next.get(msg.task_id);
        if (existing) {
          const items = [...existing.items, item].sort((a, b) => a.seq - b.seq);
          next.set(msg.task_id, { ...existing, items });
        }
        return next;
      });
    }, [issueId]),
  );

  // Task end — optimistically drop the banner for snappy UX, then
  // reconcile to also clean up sibling tasks whose own end events may
  // have been missed (e.g. a sequence of tasks all ending during a WS
  // reconnect window will only replay this one event when we resubscribe).
  const handleTaskEnd = useCallback((payload: unknown) => {
    const p = payload as { task_id: string; issue_id: string };
    if (p.issue_id !== issueId) return;
    setTaskStates((prev) => {
      if (!prev.has(p.task_id)) return prev;
      const next = new Map(prev);
      next.delete(p.task_id);
      return next;
    });
    reconcile();
  }, [issueId, reconcile]);

  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  // Newly active tasks — both queued and dispatched land here. Subscribing
  // to both events matters because retry creates a queued child without
  // emitting task:dispatch (only the daemon's claim does), so listening
  // to dispatch alone leaves the banner stale during the queued window.
  // reconcile is idempotent (per-task hydration guard) and also drops
  // stale tasks, so it's safe to fire once per event.
  const handleTaskActive = useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id && p.issue_id !== issueId) return;
    reconcile();
  }, [issueId, reconcile]);

  useWSEvent("task:queued", handleTaskActive);
  useWSEvent("task:dispatch", handleTaskActive);

  if (taskStates.size === 0) return null;

  // Order: running → dispatched → queued. The most-active task takes the
  // sticky slot; queued tasks sit below so the "is working" banner isn't
  // pushed off by a freshly-enqueued sibling. ListActiveTasksByIssue's
  // server-side ORDER BY is created_at DESC, which doesn't reflect lifecycle
  // priority, so we re-sort on the client.
  const statusRank: Record<AgentTask["status"], number> = {
    running: 0,
    dispatched: 1,
    queued: 2,
    completed: 3,
    failed: 3,
    cancelled: 3,
  };
  const entries = Array.from(taskStates.values()).sort(
    (a, b) => statusRank[a.task.status] - statusRank[b.task.status],
  );
  const [firstEntry, ...restEntries] = entries;
  if (!firstEntry) return null;

  return (
    <>
      {/* Primary agent — sticky at the top of the activity area */}
      <div className="mt-4 sticky top-4 z-10 rounded-lg bg-background/80 supports-[backdrop-filter]:bg-background/55 backdrop-blur-md">
        <SingleAgentLiveCard
          task={firstEntry.task}
          items={firstEntry.items}
          issueId={issueId}
          agentName={firstEntry.task.agent_id ? getActorName("agent", firstEntry.task.agent_id) : t(($) => $.agent_live.fallback_name)}
        />
      </div>
      {/* Additional agents — non-sticky, scroll with the page */}
      {restEntries.length > 0 && (
        <div className="mt-1.5 space-y-1.5">
          {restEntries.map(({ task, items }) => (
            <SingleAgentLiveCard
              key={task.id}
              task={task}
              items={items}
              issueId={issueId}
              agentName={task.agent_id ? getActorName("agent", task.agent_id) : t(($) => $.agent_live.fallback_name)}
            />
          ))}
        </div>
      )}
    </>
  );
}

// ─── SingleAgentLiveCard (header-only banner per active task) ──────────────

interface SingleAgentLiveCardProps {
  task: AgentTask;
  items: TimelineItem[];
  issueId: string;
  agentName: string;
}

function SingleAgentLiveCard({ task, items, issueId, agentName }: SingleAgentLiveCardProps) {
  const { t } = useT("issues");
  const [elapsed, setElapsed] = useState("");
  const [cancelling, setCancelling] = useState(false);

  const isQueued = task.status === "queued";

  // Elapsed time — ticks every second so users see the agent is alive.
  // For queued tasks neither started_at nor dispatched_at is set yet, so
  // anchor on created_at to show the "queued for Ns" wait window.
  useEffect(() => {
    const startRef = task.started_at ?? task.dispatched_at ?? task.created_at;
    if (!startRef) return;
    setElapsed(formatElapsed(startRef));
    const interval = setInterval(() => setElapsed(formatElapsed(startRef)), 1000);
    return () => clearInterval(interval);
  }, [task.started_at, task.dispatched_at, task.created_at]);

  const handleCancel = useCallback(async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.agent_live.cancel_failed));
      setCancelling(false);
    }
  }, [task.id, issueId, cancelling, t]);

  const toolCount = items.filter((i) => i.type === "tool_use").length;

  // Queued tasks render with a non-spinning Clock and dimmer accent so the
  // banner reads as "waiting" rather than "working" at a glance.
  return (
    <div className={isQueued ? "rounded-lg border border-border bg-muted/30" : "rounded-lg border border-info/20 bg-info/5"}>
      <div className="flex items-center gap-2 px-3 py-2 text-muted-foreground">
        {task.agent_id ? (
          <ActorAvatar actorType="agent" actorId={task.agent_id} size={20} enableHoverCard showStatusDot />
        ) : (
          <div className="flex items-center justify-center h-5 w-5 rounded-full shrink-0 bg-info/10 text-info">
            <Bot className="h-3 w-3" />
          </div>
        )}
        <div className="flex items-center gap-1.5 text-xs min-w-0">
          {isQueued ? (
            <Clock className="h-3 w-3 text-muted-foreground shrink-0" />
          ) : (
            <Loader2 className="h-3 w-3 animate-spin text-info shrink-0" />
          )}
          <span className="font-medium text-foreground truncate">
            {isQueued
              ? t(($) => $.agent_live.is_queued, { name: agentName })
              : t(($) => $.agent_live.is_working, { name: agentName })}
          </span>
          {/* CEREBRO-PATCH(task-title-builder): show generated task title so users
              can tell "Summary of CAC week 18" apart from "Lookup inventory" — both
              of which would otherwise just say "Lando is working" with the issue
              title only visible at the page header. */}
          {task.title && (
            <span className="text-foreground truncate" title={task.title}>{task.title}</span>
          )}
          <span className="text-muted-foreground tabular-nums shrink-0">
            {isQueued
              ? t(($) => $.agent_live.queued_elapsed_prefix, { elapsed })
              : elapsed}
          </span>
          {!isQueued && toolCount > 0 && (
            <span className="text-muted-foreground shrink-0">{t(($) => $.agent_live.tool_count, { count: toolCount })}</span>
          )}
        </div>
        <div className="ml-auto flex items-center gap-1 shrink-0">
          {!isQueued && (
            <TranscriptButton
              task={task}
              agentName={agentName}
              items={items}
              isLive
              title={t(($) => $.agent_live.transcript_button)}
            />
          )}
          <button
            onClick={handleCancel}
            disabled={cancelling}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
            title={t(($) => $.agent_live.stop_tooltip)}
          >
            {cancelling ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
            <span>{t(($) => $.agent_live.stop_button)}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── TaskRunHistory (past execution logs) ──────────────────────────────────

interface TaskRunHistoryProps {
  issueId: string;
}

export function TaskRunHistory({ issueId }: TaskRunHistoryProps) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [open, setOpen] = useState(true); // CEREBRO-PATCH(agent-runs-history-expanded-default): expand execution history on mount (JEH-1247)

  useEffect(() => {
    api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
  }, [issueId]);

  // Refresh when a task completes
  useWSEvent(
    "task:completed",
    useCallback((payload: unknown) => {
      const p = payload as TaskCompletedPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  useWSEvent(
    "task:failed",
    useCallback((payload: unknown) => {
      const p = payload as TaskFailedPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  // Refresh when a task is cancelled
  useWSEvent(
    "task:cancelled",
    useCallback((payload: unknown) => {
      const p = payload as TaskCancelledPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  const completedTasks = tasks.filter((t) => t.status === "completed" || t.status === "failed" || t.status === "cancelled");
  if (completedTasks.length === 0) return null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors py-1">
        <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
        <Clock className="h-3 w-3" />
        <span>Execution history ({completedTasks.length})</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 space-y-2">
          {completedTasks.map((task) => (
            <TaskRunEntry key={task.id} task={task} />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function TaskRunEntry({ task }: { task: AgentTask }) {
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<TimelineItem[] | null>(null);
  const [transcriptOpen, setTranscriptOpen] = useState(false);

  const loadMessages = useCallback(() => {
    if (items !== null) return; // already loaded
    api.listTaskMessages(task.id).then((msgs) => {
      setItems(buildTimeline(msgs));
    }).catch((e) => {
      console.error(e);
      setItems([]);
    });
  }, [task.id, items]);

  useEffect(() => {
    if (open) loadMessages();
  }, [open, loadMessages]);

  const duration = task.started_at && task.completed_at
    ? formatDuration(task.started_at, task.completed_at)
    : null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-accent/30 transition-colors border border-transparent hover:border-border">
        <ChevronRight className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        {task.status === "completed" ? (
          <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
        ) : (
          <XCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
        )}
        <span className="text-muted-foreground">
          {new Date(task.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
        </span>
        {duration && <span className="text-muted-foreground">{duration}</span>}
        <span className={cn("ml-auto capitalize", task.status === "completed" ? "text-success" : "text-destructive")}>
          {task.status}
        </span>
        <span
          role="button"
          tabIndex={0}
          onClick={(e) => {
            e.stopPropagation();
            // Load messages before opening the transcript dialog
            if (items === null) {
              api.listTaskMessages(task.id).then((msgs) => {
                setItems(buildTimeline(msgs));
                setTranscriptOpen(true);
              }).catch(console.error);
            } else {
              setTranscriptOpen(true);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              e.stopPropagation();
              e.currentTarget.click();
            }
          }}
          className="flex items-center justify-center rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors cursor-pointer"
          title="Expand transcript"
        >
          <Maximize2 className="h-3 w-3" />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="ml-5 mt-1 max-h-64 overflow-y-auto rounded border bg-muted/30 px-3 py-2 space-y-0.5">
          {items === null ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading...
            </div>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground py-2">No execution data recorded.</p>
          ) : (
            items.map((item, idx) => (
              <TimelineRow key={`${item.seq}-${idx}`} item={item} />
            ))
          )}
        </div>
      </CollapsibleContent>

      {/* Fullscreen transcript dialog */}
      {items !== null && (
        <AgentTranscriptDialog
          open={transcriptOpen}
          onOpenChange={setTranscriptOpen}
          task={task}
          items={items}
          agentName={task.agent_id ? getActorName("agent", task.agent_id) : "Agent"}
        />
      )}
    </Collapsible>
  );
}

// ─── Shared timeline row rendering ──────────────────────────────────────────

const ACTIVITY_ICONS: Record<string, { icon: string; color: string }> = {
  decision: { icon: "◆", color: "text-purple-500" },
  verification: { icon: "✓", color: "text-success" },
  blocker: { icon: "⚠", color: "text-warning" },
  dependency: { icon: "⬡", color: "text-blue-500" },
  error: { icon: "✕", color: "text-destructive" },
  note: { icon: "●", color: "text-muted-foreground" },
};

function ActivityRow({ item }: { item: TimelineItem }) {
  const cfg = ACTIVITY_ICONS[item.type] ?? { icon: "●", color: "text-muted-foreground" };
  // Content format: "[type] summary\ndetails"
  const content = item.content ?? "";
  const withoutPrefix = content.replace(/^\[[^\]]*\]\s*/, "");
  const [summary, ...detailLines] = withoutPrefix.split("\n");
  const details = detailLines.join("\n").trim();
  const files = (item.input as Record<string, unknown>)?.files as string[] | undefined;

  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <span className={cn("shrink-0 mt-0.5 font-mono", cfg.color)}>{cfg.icon}</span>
      <div className="min-w-0">
        <span className="font-medium">{summary}</span>
        {details && <p className="text-muted-foreground mt-0.5 whitespace-pre-wrap">{details}</p>}
        {files && files.length > 0 && (
          <p className="text-muted-foreground mt-0.5 font-mono text-[10px]">{files.join(", ")}</p>
        )}
      </div>
    </div>
  );
}

function TimelineRow({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "thinking":
      return <ThinkingRow item={item} />;
    case "text":
      return <TextRow item={item} />;
    case "error":
      return <ErrorRow item={item} />;
    // Activity types from report_activity
    case "decision":
    case "verification":
    case "blocker":
    case "dependency":
    case "note":
      return <ActivityRow item={item} />;
    default:
      // Fallback for unknown types — show as text
      if (item.content) return <TextRow item={item} />;
      return null;
  }
}

function ToolCallRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  // Safe: ToolCallRow is only rendered for items with type === "tool_use",
  // which is in ChatTimelineItem's narrower union.
  const summary = getToolSummary(item as Parameters<typeof getToolSummary>[0]);
  const hasInput = item.input && Object.keys(item.input).length > 0;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
            !hasInput && "invisible",
          )}
        />
        <span className="font-medium text-foreground shrink-0">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
      </CollapsibleTrigger>
      {hasInput && (
        <CollapsibleContent>
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
            {redactSecrets(JSON.stringify(item.input, null, 2))}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  const output = item.output ?? "";
  if (!output) return null;

  const preview = output.length > 120 ? output.slice(0, 120) + "..." : output;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform mt-0.5", open && "rotate-90")}
        />
        <span className="text-muted-foreground/70 truncate">
          {item.tool ? `${item.tool} result: ` : "result: "}{preview}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {output.length > 4000 ? output.slice(0, 4000) + "\n... (truncated)" : output}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ThinkingRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  const text = item.content ?? "";
  if (!text) return null;

  const preview = text.length > 150 ? text.slice(0, 150) + "..." : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-info/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-info/5 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function TextRow({ item }: { item: TimelineItem }) {
  const text = item.content ?? "";
  if (!text.trim()) return null;
  const lines = text.trim().split("\n").filter(Boolean);
  const last = lines[lines.length - 1] ?? "";
  if (!last) return null;

  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <span className="h-3 w-3 shrink-0" />
      <span className="text-muted-foreground/60 truncate">{last}</span>
    </div>
  );
}

function ErrorRow({ item }: { item: TimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}

// ─── Work Session History (Claude Code MCP sessions) ────────────────────────

interface WorkSessionHistoryProps {
  issueId: string;
}

export function WorkSessionHistory({ issueId }: WorkSessionHistoryProps) {
  const [sessions, setSessions] = useState<WorkSession[]>([]);

  const refresh = useCallback(() => {
    api.listWorkSessions(issueId).then(setSessions).catch(console.error);
  }, [issueId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const copyStartCommand = () => {
    const cmd = `claude --resume "Work on issue ${issueId}"`;
    navigator.clipboard.writeText(cmd).then(() => {
      toast.success("Command copied — paste in terminal to start a Claude Code session");
    }).catch(() => {
      toast.error("Failed to copy");
    });
  };

  return (
    <div className="space-y-2">
      <button
        type="button"
        onClick={copyStartCommand}
        className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
      >
        <Plus className="h-3 w-3" />
        <span>Start new session</span>
      </button>
      {sessions.map((ws) => (
        <WorkSessionEntry key={ws.id} session={ws} onUpdate={refresh} />
      ))}
      {sessions.length === 0 && (
        <p className="text-xs text-muted-foreground py-2">No sessions yet. Start one from your terminal.</p>
      )}
    </div>
  );
}

function WorkSessionEntry({ session, onUpdate }: { session: WorkSession; onUpdate: () => void }) {
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<TimelineItem[] | null>(null);

  const loadMessages = useCallback(() => {
    if (items !== null) return;
    api.getWorkSessionMessages(session.id).then((msgs) => {
      setItems(buildTimeline(msgs));
    }).catch((e) => {
      console.error(e);
      setItems([]);
    });
  }, [session.id, items]);

  useEffect(() => {
    if (open) loadMessages();
  }, [open, loadMessages]);

  const handleStop = (e: React.MouseEvent) => {
    e.stopPropagation();
    api.completeWorkSession(session.id, "Stopped from UI").then(() => {
      toast.success("Session stopped");
      onUpdate();
    }).catch(() => toast.error("Failed to stop session"));
  };

  const handleResume = (e: React.MouseEvent) => {
    e.stopPropagation();
    api.resumeWorkSession(session.id).then(() => {
      toast.success("Session resumed — next Claude Code session will auto-attach");
      onUpdate();
    }).catch(() => toast.error("Failed to resume session"));
  };

  const handleFork = (e: React.MouseEvent) => {
    e.stopPropagation();
    api.forkWorkSession(session.id).then(() => {
      toast.success("Session forked — next Claude Code session will auto-attach");
      onUpdate();
    }).catch(() => toast.error("Failed to fork session"));
  };

  const statusColor = session.status === "completed" ? "text-success"
    : session.status === "active" ? "text-blue-500"
    : "text-destructive";

  const statusIcon = session.status === "completed" ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
    : session.status === "active" ? <span className="relative flex h-3.5 w-3.5 shrink-0 items-center justify-center"><span className="absolute size-2 rounded-full bg-blue-500 animate-ping opacity-40" /><span className="size-2 rounded-full bg-blue-500" /></span>
    : <XCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />;

  const userName = getActorName("member", session.user_id);
  const isActive = session.status === "active";
  const canResume = session.status === "completed" || session.status === "failed";

  const duration = session.completed_at
    ? formatDuration(session.created_at, session.completed_at)
    : null;

  const displayName = session.name ?? new Date(session.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-accent/30 transition-colors border border-transparent hover:border-border">
        <ChevronRight className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        {statusIcon}
        <span className="truncate font-medium">{displayName}</span>
        <span className="text-muted-foreground shrink-0">{userName}</span>
        {session.branch && <span className="text-[10px] text-muted-foreground font-mono shrink-0">{session.branch}</span>}
        {duration && <span className="text-muted-foreground shrink-0">{duration}</span>}
        <span className={cn("ml-auto capitalize shrink-0", statusColor)}>
          {session.status}
        </span>
        {isActive && (
          <span
            role="button"
            tabIndex={0}
            onClick={handleStop}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); handleStop(e as unknown as React.MouseEvent); } }}
            className="flex items-center justify-center rounded p-0.5 text-muted-foreground hover:text-destructive hover:bg-accent/50 transition-colors cursor-pointer"
            title="Stop session"
          >
            <Square className="h-3 w-3" />
          </span>
        )}
        {canResume && (
          <>
            <span
              role="button"
              tabIndex={0}
              onClick={handleResume}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); handleResume(e as unknown as React.MouseEvent); } }}
              className="flex items-center justify-center rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors cursor-pointer"
              title="Resume session"
            >
              <Play className="h-3 w-3" />
            </span>
            <span
              role="button"
              tabIndex={0}
              onClick={handleFork}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); handleFork(e as unknown as React.MouseEvent); } }}
              className="flex items-center justify-center rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors cursor-pointer"
              title="Fork session"
            >
              <GitFork className="h-3 w-3" />
            </span>
          </>
        )}
      </CollapsibleTrigger>
      <CollapsibleContent>
        {session.summary && (
          <p className="ml-5 mt-1 text-xs text-muted-foreground italic">{session.summary}</p>
        )}
        <div className="ml-5 mt-1 max-h-64 overflow-y-auto rounded border bg-muted/30 px-3 py-2 space-y-0.5">
          {items === null ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading...
            </div>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground py-2">No execution data recorded.</p>
          ) : (
            items.map((item, idx) => (
              <TimelineRow key={`${item.seq}-${idx}`} item={item} />
            ))
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

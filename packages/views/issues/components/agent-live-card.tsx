"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Bot, Clock, Loader2, Square } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { TaskMessagePayload } from "@multica/core/types/events";
import type { AgentTask } from "@multica/core/types/agent";
import { toast } from "sonner";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  TranscriptButton,
  buildTimeline,
  type TimelineItem,
} from "../../common/task-transcript";
import { useT } from "../../i18n";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";

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
  const [confirmOpen, setConfirmOpen] = useState(false);

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

  const requestCancel = useCallback(() => {
    if (cancelling) return;
    setConfirmOpen(true);
  }, [cancelling]);

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
            onClick={requestCancel}
            disabled={cancelling}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
            title={t(($) => $.agent_live.stop_tooltip)}
          >
            {cancelling ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
            <span>{t(($) => $.agent_live.stop_button)}</span>
          </button>
        </div>
      </div>
      <TerminateTaskConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleCancel()}
        showRunningNote={!isQueued}
      />
    </div>
  );
}

"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Bot, Loader2, Square } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
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
  const { getActorName } = useActorName();
  const [taskStates, setTaskStates] = useState<Map<string, TaskState>>(new Map());
  const seenSeqs = useRef(new Set<string>());

  // Fetch active tasks on mount
  useEffect(() => {
    let cancelled = false;
    api.getActiveTasksForIssue(issueId).then(({ tasks }) => {
      if (cancelled || tasks.length === 0) return;

      // Show cards immediately with empty timeline
      setTaskStates((prev) => {
        const next = new Map(prev);
        for (const task of tasks) {
          if (!next.has(task.id)) {
            next.set(task.id, { task, items: [] });
          }
        }
        return next;
      });

      // Load messages per task in the background — these feed the live
      // TranscriptButton, not an inline timeline (timeline UI moved to
      // the right panel).
      for (const task of tasks) {
        api.listTaskMessages(task.id).then((msgs) => {
          if (cancelled) return;
          const timeline = buildTimeline(msgs);
          for (const m of msgs) seenSeqs.current.add(`${m.task_id}:${m.seq}`);
          setTaskStates((prev) => {
            const next = new Map(prev);
            const existing = next.get(task.id);
            if (existing) {
              const loadedSeqs = new Set(timeline.map((i) => i.seq));
              const wsOnly = existing.items.filter((i) => !loadedSeqs.has(i.seq));
              const merged = [...timeline, ...wsOnly].sort((a, b) => a.seq - b.seq);
              next.set(task.id, { task: existing.task, items: merged });
            } else {
              next.set(task.id, { task, items: timeline });
            }
            return next;
          });
        }).catch(console.error);
      }
    }).catch(console.error);

    return () => { cancelled = true; };
  }, [issueId]);

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

  // Task end — drop the banner. The right-panel ExecutionLogSection
  // will pick the same task back up under "Past runs" via its own WS
  // invalidate path.
  const handleTaskEnd = useCallback((payload: unknown) => {
    const p = payload as { task_id: string; issue_id: string };
    if (p.issue_id !== issueId) return;
    setTaskStates((prev) => {
      const next = new Map(prev);
      next.delete(p.task_id);
      return next;
    });
  }, [issueId]);

  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  // Newly active tasks — both queued and dispatched land here. Subscribing
  // to both events matters because retry creates a queued child without
  // emitting task:dispatch (only the daemon's claim does), so listening
  // to dispatch alone leaves the banner stale during the queued window.
  // The handler is idempotent (only inserts unseen task IDs), so it's
  // safe to fire once per event even when both arrive in quick succession.
  const handleTaskActive = useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id && p.issue_id !== issueId) return;
    api.getActiveTasksForIssue(issueId).then(({ tasks }) => {
      setTaskStates((prev) => {
        const next = new Map(prev);
        for (const task of tasks) {
          if (!next.has(task.id)) {
            next.set(task.id, { task, items: [] });
          }
        }
        return next;
      });
    }).catch(console.error);
  }, [issueId]);

  useWSEvent("task:queued", handleTaskActive);
  useWSEvent("task:dispatch", handleTaskActive);

  if (taskStates.size === 0) return null;

  const entries = Array.from(taskStates.values());
  const [firstEntry, ...restEntries] = entries;
  if (!firstEntry) return null;

  return (
    <>
      {/* Primary agent — sticky at the top of the activity area */}
      <div className="mt-4 sticky top-4 z-10">
        <SingleAgentLiveCard
          task={firstEntry.task}
          items={firstEntry.items}
          issueId={issueId}
          agentName={firstEntry.task.agent_id ? getActorName("agent", firstEntry.task.agent_id) : "Agent"}
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
              agentName={task.agent_id ? getActorName("agent", task.agent_id) : "Agent"}
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
  const [elapsed, setElapsed] = useState("");
  const [cancelling, setCancelling] = useState(false);

  // Elapsed time — ticks every second so users see the agent is alive.
  useEffect(() => {
    if (!task.started_at && !task.dispatched_at) return;
    const startRef = task.started_at ?? task.dispatched_at!;
    setElapsed(formatElapsed(startRef));
    const interval = setInterval(() => setElapsed(formatElapsed(startRef)), 1000);
    return () => clearInterval(interval);
  }, [task.started_at, task.dispatched_at]);

  const handleCancel = useCallback(async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to cancel task");
      setCancelling(false);
    }
  }, [task.id, issueId, cancelling]);

  const toolCount = items.filter((i) => i.type === "tool_use").length;

  return (
    <div className="rounded-lg border border-info/20 bg-info/5 backdrop-blur-sm">
      <div className="flex items-center gap-2 px-3 py-2 text-muted-foreground">
        {task.agent_id ? (
          <ActorAvatar actorType="agent" actorId={task.agent_id} size={20} enableHoverCard showStatusDot />
        ) : (
          <div className="flex items-center justify-center h-5 w-5 rounded-full shrink-0 bg-info/10 text-info">
            <Bot className="h-3 w-3" />
          </div>
        )}
        <div className="flex items-center gap-1.5 text-xs min-w-0">
          <Loader2 className="h-3 w-3 animate-spin text-info shrink-0" />
          <span className="font-medium text-foreground truncate">{agentName} is working</span>
          <span className="text-muted-foreground tabular-nums shrink-0">{elapsed}</span>
          {toolCount > 0 && (
            <span className="text-muted-foreground shrink-0">{toolCount} tools</span>
          )}
        </div>
        <div className="ml-auto flex items-center gap-1 shrink-0">
          <TranscriptButton
            task={task}
            agentName={agentName}
            items={items}
            isLive
            title="View transcript"
          />
          <button
            onClick={handleCancel}
            disabled={cancelling}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
            title="Stop agent"
          >
            {cancelling ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
            <span>Stop</span>
          </button>
        </div>
      </div>
    </div>
  );
}

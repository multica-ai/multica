"use client";

// Inline "agent is working" rows rendered between the last comment and
// the message input on a channel. Until JEH-698 there was no signal in
// the channel UI that an agent had been triggered — the user posted
// "@Lando lav et summary…" and then waited blindly until Lando's reply
// landed seconds later. This component closes that gap: every active
// task on the channel renders a one-line row with avatar, "is working",
// the generated task title, elapsed time, and a Stop control.
//
// Data flow mirrors AgentLiveCard (issue detail) — the channel is just
// an issue under the hood, so /api/issues/:id/active-task and the
// task:queued / task:dispatch / task:completed / task:failed /
// task:cancelled WS events all work for channels too. We keep this
// component slim (no transcript dialog, no message hydration) because
// the channel comment stream itself is the agent's transcript — the
// agent's reply lands as a comment.

import { useCallback, useEffect, useRef, useState, type ComponentType } from "react";
import { Loader2, Square } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { AgentTask } from "@multica/core/types/agent";
import { useActorName } from "@multica/core/workspace/hooks";

// Avatar component is injected by the consumer (views/channels) so cerebro-channels
// stays free of an `@multica/views` dependency — that direction would close a
// cycle once views imports favorites-store from cerebro-channels.
type AvatarComponent = ComponentType<{
  actorType: "agent" | "member";
  actorId: string;
  size?: number;
  enableHoverCard?: boolean;
  showStatusDot?: boolean;
}>;

interface ChannelAgentInlineRowProps {
  channelId: string;
  AvatarComponent: AvatarComponent;
}

export function ChannelAgentInlineRow({ channelId, AvatarComponent }: ChannelAgentInlineRowProps) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const reconcileSeq = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const reconcile = useCallback(() => {
    const mySeq = ++reconcileSeq.current;
    api
      .getActiveTasksForIssue(channelId)
      .then(({ tasks: next }) => {
        if (!mountedRef.current) return;
        // Drop stale responses if a newer reconcile has been issued —
        // same monotonic-counter trick AgentLiveCard uses, prevents a
        // slow request from overwriting a fresher empty result.
        if (mySeq !== reconcileSeq.current) return;
        setTasks(next);
      })
      .catch((err) => {
        // Active-tasks lookup is a soft signal — if it fails we just
        // hide the inline row, never block the channel from rendering.
        console.error("ChannelAgentInlineRow: active tasks fetch failed", err);
      });
  }, [channelId]);

  useEffect(() => {
    reconcile();
  }, [reconcile]);

  // Re-pull on WS reconnect — task end events emitted while we were
  // offline don't replay, so the row would otherwise stay stuck at
  // "is working" for a task that finished long ago.
  useWSReconnect(reconcile);

  // Add a task as soon as it's queued/dispatched. reconcile is
  // idempotent + always reconciles to server truth so no need for
  // optimistic local merging.
  const handleTaskActive = useCallback(
    (payload: unknown) => {
      const p = payload as { issue_id?: string };
      if (p.issue_id && p.issue_id !== channelId) return;
      reconcile();
    },
    [channelId, reconcile],
  );
  useWSEvent("task:queued", handleTaskActive);
  useWSEvent("task:dispatch", handleTaskActive);

  // Optimistic drop on terminal events — reconcile catches sibling
  // tasks whose own end events may have been missed during a reconnect.
  const handleTaskEnd = useCallback(
    (payload: unknown) => {
      const p = payload as { task_id: string; issue_id: string };
      if (p.issue_id !== channelId) return;
      setTasks((prev) => prev.filter((t) => t.id !== p.task_id));
      reconcile();
    },
    [channelId, reconcile],
  );
  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  if (tasks.length === 0) return null;

  return (
    <div className="space-y-1.5">
      {tasks.map((task) => (
        <ChannelAgentRow
          key={task.id}
          task={task}
          channelId={channelId}
          AvatarComponent={AvatarComponent}
        />
      ))}
    </div>
  );
}

// ─── Single row ───────────────────────────────────────────────────────────

function formatElapsed(startedAt: string | null | undefined, fallback: string): string {
  const start = startedAt ?? fallback;
  if (!start) return "";
  const ms = Date.now() - new Date(start).getTime();
  const seconds = Math.floor(ms / 1000);
  if (seconds < 1) return "0s";
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

interface ChannelAgentRowProps {
  task: AgentTask;
  channelId: string;
  AvatarComponent: AvatarComponent;
}

function ChannelAgentRow({ task, channelId, AvatarComponent }: ChannelAgentRowProps) {
  const { getActorName } = useActorName();
  const agentName = task.agent_id ? getActorName("agent", task.agent_id) : "Agent";
  const [elapsed, setElapsed] = useState("");
  const [cancelling, setCancelling] = useState(false);

  // Tick the elapsed counter every second so the user sees the agent
  // is alive. Anchor on started_at when running, dispatched_at when
  // dispatched, fall back to created_at while still queued so the
  // counter doesn't read "" for a freshly-queued task.
  useEffect(() => {
    const start = task.started_at ?? task.dispatched_at ?? task.created_at;
    if (!start) return;
    setElapsed(formatElapsed(start, task.created_at));
    const interval = setInterval(() => setElapsed(formatElapsed(start, task.created_at)), 1000);
    return () => clearInterval(interval);
  }, [task.started_at, task.dispatched_at, task.created_at]);

  const handleCancel = useCallback(async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(channelId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to stop");
      setCancelling(false);
    }
  }, [task.id, channelId, cancelling]);

  return (
    <div className="rounded-lg border border-info/20 bg-info/5">
      <div className="flex items-center gap-2 px-3 py-2 text-muted-foreground">
        {task.agent_id ? (
          <AvatarComponent
            actorType="agent"
            actorId={task.agent_id}
            size={20}
            enableHoverCard
            showStatusDot
          />
        ) : null}
        <div className="flex min-w-0 items-center gap-1.5 text-xs">
          <Loader2 className="h-3 w-3 shrink-0 animate-spin text-info" />
          <span className="shrink-0 font-medium text-foreground">
            {`${agentName} is working`}
          </span>
          {task.title && (
            <span className="truncate text-foreground" title={task.title}>
              {task.title}
            </span>
          )}
          <span className="shrink-0 tabular-nums text-muted-foreground">{elapsed}</span>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          <button
            onClick={handleCancel}
            disabled={cancelling}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
            title="Stop the agent"
            type="button"
          >
            {cancelling ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Square className="h-3 w-3" />
            )}
            <span>Stop</span>
          </button>
        </div>
      </div>
    </div>
  );
}

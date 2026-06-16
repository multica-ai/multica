"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Bot,
  CheckCircle2,
  Clock,
  Loader2,
  ScrollText,
  X,
  XCircle,
  Ban,
} from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useAgentPresenceDetail } from "@multica/core/agents/use-agent-presence";
import type { AgentTask, ChatPendingTask } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { ChatMessageList } from "./chat-message-list";
import { buildRunChat, deriveElapsedMs, isTerminalRun } from "../lib/build-run-chat";
import { formatElapsedMs } from "../lib/format";

interface VisualRunHistoryProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  /**
   * Display name for the run's agent. The execution-log rows don't carry one
   * (they render the avatar's own hover-card), so we resolve it ourselves when
   * blank — see `useResolvedAgentName`.
   */
  agentName?: string;
  /**
   * Switch back to the raw (non-visual) log dialog. The visual view is an
   * opt-in lens over the same run; the escape hatch lets a user drop to the
   * dense event log without leaving the row. Omit to hide the affordance.
   */
  onViewRawLog?: () => void;
}

// Resolve a human name for the header. Prefer the caller-supplied name; fall
// back to a one-shot fetch (mirrors AgentTranscriptDialog), and finally to a
// generic localized label so the header never renders blank.
function useResolvedAgentName(
  open: boolean,
  agentId: string | null | undefined,
  provided: string | undefined,
  fallback: string,
): string {
  const [fetched, setFetched] = useState<string | null>(null);
  useEffect(() => {
    if (!open || provided || !agentId) return;
    let cancelled = false;
    api
      .getAgent(agentId)
      .then((agent) => {
        if (!cancelled) setFetched(agent.name);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [open, provided, agentId]);
  return provided?.trim() || fetched || fallback;
}

/**
 * Read-only, chat-style preview of a single agent run. Reuses the live-chat
 * renderer (`ChatMessageList`) so a completed or in-progress run reads as a
 * conversation rather than a raw event log. The view is strictly read-only —
 * there is no composer and no per-message actions beyond copy.
 *
 *  - Terminal run: static. Messages = user trigger + one assistant bubble that
 *    re-renders the full timeline. Anchored to the TOP so the supervisor reads
 *    the run from the beginning.
 *  - In-flight run: live. Messages = user trigger only; the streaming timeline
 *    + status pill render through `ChatMessageList`'s `pendingTask` slot, which
 *    keeps the list bottom-anchored and auto-following new output.
 */
export function VisualRunHistory({
  open,
  onOpenChange,
  task,
  agentName,
  onViewRawLog,
}: VisualRunHistoryProps) {
  const { t } = useT("agents");
  const ws = useCurrentWorkspace();

  const terminal = isTerminalRun(task);
  const resolvedName = useResolvedAgentName(
    open,
    task.agent_id,
    agentName,
    t(($) => $.visual_run.agent_fallback),
  );

  // Presence drives the live status-pill copy (offline / reconnecting / etc.).
  // Only meaningful for an in-flight run; terminal runs render no pill.
  const presenceDetail = useAgentPresenceDetail(
    ws?.id,
    terminal ? undefined : (task.agent_id ?? undefined),
  );
  const availability =
    presenceDetail === "loading" ? undefined : presenceDetail.availability;

  const messages = useMemo(() => buildRunChat(task), [task]);

  // In-flight: hand the chat list a pending-task snapshot so it streams the
  // live timeline + pill. Terminal: null, so the list is fully static.
  const pendingTask: ChatPendingTask | null = terminal
    ? null
    : {
        task_id: task.id,
        status: task.status,
        created_at:
          task.started_at ?? task.dispatched_at ?? task.created_at,
      };

  // Live elapsed timer for the header chip. Terminal runs use the fixed
  // server-derived duration; in-flight runs tick once a second.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!open || terminal) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [open, terminal]);

  const durationLabel = useMemo(() => {
    if (terminal) {
      const ms = deriveElapsedMs(task);
      return ms != null ? formatElapsedMs(ms) : null;
    }
    const startIso =
      task.started_at ?? task.dispatched_at ?? task.created_at;
    if (!startIso) return null;
    const ms = now - new Date(startIso).getTime();
    return Number.isFinite(ms) && ms > 0 ? formatElapsedMs(ms) : null;
  }, [terminal, task, now]);

  // A run with no trigger snapshot AND no terminal assistant bubble would
  // render an empty list. buildRunChat returns [] only in that case (live run
  // with no trigger); show a mode-aware empty state instead.
  const isEmpty = messages.length === 0;

  // Final-outcome line for assistive tech (terminal runs only). A static
  // transcript has no streaming pill to carry the outcome, so we announce it
  // once via an sr-only role="status" region.
  const durationSuffix = durationLabel
    ? t(($) => $.visual_run.duration_suffix, { duration: durationLabel })
    : "";
  const outcomeText =
    task.status === "failed"
      ? t(($) => $.visual_run.outcome_failed, { duration: durationSuffix })
      : task.status === "cancelled"
        ? t(($) => $.visual_run.outcome_cancelled, { duration: durationSuffix })
        : t(($) => $.visual_run.outcome_completed, { duration: durationSuffix });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-3xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">
          {t(($) => $.visual_run.dialog_title)}
        </DialogTitle>

        {/* ── Header ─────────────────────────────────────────────── */}
        <div className="flex items-center gap-3 border-b px-4 py-3 shrink-0">
          <div className="flex min-w-0 items-center gap-2">
            {task.agent_id ? (
              <ActorAvatar actorType="agent" actorId={task.agent_id} size={24} />
            ) : (
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-info/10 text-info">
                <Bot className="h-3.5 w-3.5" />
              </div>
            )}
            <span className="truncate text-sm font-medium">{resolvedName}</span>
          </div>

          <RunStatusBadge task={task} terminal={terminal} />

          {durationLabel && (
            <span className="inline-flex items-center gap-1 rounded-md border bg-muted/50 px-2 py-0.5 text-[11px] text-muted-foreground">
              <Clock className="h-3 w-3" />
              {durationLabel}
            </span>
          )}

          <div className="ml-auto flex items-center gap-1">
            {onViewRawLog && (
              <button
                type="button"
                onClick={onViewRawLog}
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <ScrollText className="h-3.5 w-3.5" />
                {t(($) => $.visual_run.view_raw_log)}
              </button>
            )}
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              aria-label={t(($) => $.visual_run.close)}
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* ── Final-outcome announcement (terminal only) ─────────────
            Politely announced to assistive tech once, since a static
            transcript has no streaming pill to carry the outcome. */}
        {terminal && (
          <div role="status" className="sr-only">
            {outcomeText}
          </div>
        )}

        {/* ── Body ───────────────────────────────────────────────── */}
        {isEmpty ? (
          <div className="flex flex-1 items-center justify-center px-6 text-center text-sm text-muted-foreground">
            {terminal ? (
              t(($) => $.visual_run.empty_terminal)
            ) : (
              <span className="inline-flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                {t(($) => $.visual_run.empty_live)}
              </span>
            )}
          </div>
        ) : (
          <div className="flex flex-1 flex-col overflow-hidden">
            {task.status === "cancelled" && (
              <div className="flex items-center gap-1.5 border-b bg-muted/20 px-5 py-2 text-xs text-muted-foreground">
                <Ban className="h-3.5 w-3.5 shrink-0" />
                {t(($) => $.visual_run.cancelled_note)}
              </div>
            )}
            <ChatMessageList
              messages={messages}
              pendingTask={pendingTask}
              availability={availability}
              // Terminal → read from the top. Live → leave undefined so the
              // list keeps its bottom-anchor + auto-follow for new output.
              initialTopMostItemIndex={terminal ? 0 : undefined}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// Header status badge — mirrors the AgentTranscriptDialog tone vocabulary so
// the two views read consistently. Terminal runs map by status; an in-flight
// run is always "running".
function RunStatusBadge({
  task,
  terminal,
}: {
  task: AgentTask;
  terminal: boolean;
}) {
  const { t } = useT("agents");
  if (!terminal) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-info/15 px-2 py-0.5 text-xs font-medium text-info">
        <Loader2 className="h-3 w-3 animate-spin" />
        {t(($) => $.visual_run.status_running)}
      </span>
    );
  }
  if (task.status === "completed") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium text-success">
        <CheckCircle2 className="h-3 w-3" />
        {t(($) => $.visual_run.status_completed)}
      </span>
    );
  }
  if (task.status === "failed") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-destructive/15 px-2 py-0.5 text-xs font-medium text-destructive">
        <XCircle className="h-3 w-3" />
        {t(($) => $.visual_run.status_failed)}
      </span>
    );
  }
  // cancelled (the only remaining terminal status)
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
      <Ban className="h-3 w-3" />
      {t(($) => $.visual_run.status_cancelled)}
    </span>
  );
}

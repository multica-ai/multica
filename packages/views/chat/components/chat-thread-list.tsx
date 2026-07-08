"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  ChevronLeft,
  ChevronRight,
  Clock,
  Loader2,
  Pin,
  PinOff,
  Square,
  Trash2,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { pendingChatTasksOptions, chatKeys, sortChatSessions } from "@multica/core/chat/queries";
import {
  useDeleteChatSession,
  useSetChatSessionArchived,
  useSetChatSessionPinned,
} from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import type { Agent, ChatSession, PendingChatTasksResponse } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { createLogger } from "@multica/core/logger";
import { removeChatMessageFromCaches } from "./use-chat-controller";
import { useT } from "../../i18n";

const apiLogger = createLogger("chat.api");

// IM-style timestamp: today → clock, this year → M/D, else full date.
function formatChatTime(dateStr: string): string {
  const d = new Date(dateStr);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString([], { month: "numeric", day: "numeric" });
  }
  return d.toLocaleDateString();
}

// Collapse a (possibly markdown / multi-line) message into a one-line preview.
function toPreview(content: string): string {
  return content
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*`>~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * IM-style conversation list: each row is agent avatar + name + last-message
 * preview + time, with a red unread *count* badge. An in-flight agent shows a
 * "typing…" indicator; a failed last reply shows a destructive hint. Rows are
 * rendered in the server's order (most-recent activity first). Renaming lives
 * in the conversation header's ⋯ menu, not here.
 *
 * Two views, toggled locally: the default "history" view lists active chats and
 * hovering a row reveals pin + archive (or stop, while running) — archiving is
 * the reversible, one-click default so nothing is destroyed by accident. A
 * footer entry ("Archived · N") switches to the "archived" view, which lists
 * archived chats and is the ONLY place a chat can be hard-deleted (hover →
 * unarchive + delete). Both views read the same flat sessions cache and split
 * on `status` locally.
 */
export function ChatThreadList({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (session: ChatSession) => void;
}) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  // Split the flat cache locally: active chats fill the default history view,
  // archived chats fill the "Archived" view. Both sorted pinned-first (then by
  // activity) so the list stays ordered even after an optimistic pin/archive or
  // a WS patch mutates the flat cache in place.
  const historySessions = useMemo(
    () => sortChatSessions(sessions.filter((s) => s.status !== "archived")),
    [sessions],
  );
  const archivedSessions = useMemo(
    () => sortChatSessions(sessions.filter((s) => s.status === "archived")),
    [sessions],
  );

  // Which view is showing. Falls back to history when the archived list drains
  // (last chat unarchived / deleted) so we never strand the user on an empty
  // archive.
  const [view, setView] = useState<"history" | "archived">("history");
  useEffect(() => {
    if (view === "archived" && archivedSessions.length === 0) setView("history");
  }, [view, archivedSessions.length]);

  const [confirmingDeleteId, setConfirmingDeleteId] = useState<string | null>(null);
  const [confirmingStopId, setConfirmingStopId] = useState<string | null>(null);
  const [stoppingTaskId, setStoppingTaskId] = useState<string | null>(null);
  const deleteSession = useDeleteChatSession();
  const setPinned = useSetChatSessionPinned();
  const setArchived = useSetChatSessionArchived();
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const queryClient = useQueryClient();

  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const pendingTaskBySessionId = useMemo(
    () => new Map((pending?.tasks ?? []).map((task) => [task.chat_session_id, task])),
    [pending],
  );

  // Per-agent presence, so a pending task on an OFFLINE agent shows "waiting"
  // rather than a misleading "typing…" (the task is queued until the agent is
  // back). Same availability source the conversation pane's status pill uses,
  // keeping the two surfaces consistent.
  const presence = useWorkspacePresenceMap(wsId);

  useEffect(() => {
    if (!confirmingStopId || pendingTaskBySessionId.has(confirmingStopId)) return;
    setConfirmingStopId(null);
  }, [confirmingStopId, pendingTaskBySessionId]);

  const handleConfirmDelete = (session: ChatSession) => {
    const sessionId = session.id;
    if (activeSessionId === sessionId) setActiveSession(null);
    deleteSession.mutate(sessionId, {
      onSettled: () => setConfirmingDeleteId(null),
    });
  };

  const handleConfirmStop = (
    session: ChatSession,
    task: PendingChatTasksResponse["tasks"][number],
  ) => {
    setStoppingTaskId(task.task_id);
    queryClient.setQueryData<PendingChatTasksResponse>(chatKeys.pendingTasks(wsId), (current) => {
      if (!current) return current;
      return {
        ...current,
        tasks: current.tasks.filter((item) => item.task_id !== task.task_id),
      };
    });
    queryClient.setQueryData(chatKeys.pendingTask(session.id), {});
    queryClient.invalidateQueries({ queryKey: chatKeys.messages(session.id) });
    queryClient.invalidateQueries({ queryKey: chatKeys.messagesPage(session.id) });

    api.cancelTaskById(task.task_id).then(
      (result) => {
        const restored = result.cancelled_chat_message;
        if (restored?.restore_to_input) {
          removeChatMessageFromCaches(queryClient, restored.chat_session_id, restored.message_id);
        }
        apiLogger.info("cancelTask.success (list row)", { taskId: task.task_id, sessionId: session.id });
      },
      (err) =>
        apiLogger.warn("cancelTask.error (list row; task may have already finished)", {
          taskId: task.task_id,
          sessionId: session.id,
          err,
        }),
    ).finally(() => {
      queryClient.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
      queryClient.invalidateQueries({ queryKey: chatKeys.pendingTask(session.id) });
      setStoppingTaskId(null);
      setConfirmingStopId(null);
    });
  };

  const renderRow = (session: ChatSession) => {
    const isCurrent = session.id === activeSessionId;
    const agent = agentById.get(session.agent_id) ?? null;
    const pendingTask = pendingTaskBySessionId.get(session.id);
    const isRunning = !!pendingTask;
    // Only "offline" (definitively long-offline) downgrades typing → waiting.
    // Unknown/loading presence keeps the optimistic "typing…" so we never
    // suppress it just because presence data hasn't landed yet.
    const agentOffline = agent
      ? presence.byAgent.get(agent.id)?.availability === "offline"
      : false;
    const unread = isCurrent ? 0 : (session.unread_count ?? 0);
    const isConfirmingDelete = confirmingDeleteId === session.id;
    const isConfirmingStop = confirmingStopId === session.id && !!pendingTask;
    const isConfirmingAction = isConfirmingDelete || isConfirmingStop;
    const titleText = session.title?.trim() || t(($) => $.window.untitled);
    const last = session.last_message ?? null;
    const timeText = last ? formatChatTime(last.created_at) : formatChatTime(session.updated_at);

    // The second line: typing/waiting → failed → preview.
    let previewNode: React.ReactNode;
    if (isRunning && agentOffline) {
      // Task is queued but the agent is offline — it will run once the agent
      // is back. Show a static "waiting", not an animated "typing".
      previewNode = (
        <span className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
          <Clock className="size-3 shrink-0" />
          <span className="truncate">{t(($) => $.list.waiting)}</span>
        </span>
      );
    } else if (isRunning) {
      previewNode = (
        <span className="flex min-w-0 items-center gap-1.5 text-emerald-500">
          <Loader2 className="size-3 shrink-0 animate-spin" />
          <span className="truncate">{t(($) => $.list.typing)}</span>
        </span>
      );
    } else if (last?.failure_reason) {
      previewNode = <span className="block truncate text-destructive">{t(($) => $.list.failed)}</span>;
    } else if (last) {
      previewNode = (
        <span className={cn("block truncate", unread > 0 ? "text-foreground/80" : "text-muted-foreground")}>
          {last.role === "user" ? t(($) => $.list.you_prefix) : ""}
          {toPreview(last.content)}
        </span>
      );
    } else {
      previewNode = <span className="block truncate text-muted-foreground/60">{t(($) => $.list.no_messages)}</span>;
    }

    return (
      <div
        key={session.id}
        aria-current={isCurrent ? "true" : undefined}
        tabIndex={0}
        onClick={() => {
          if (isConfirmingAction) return;
          onSelectSession(session);
        }}
        onKeyDown={(e) => {
          if (isConfirmingAction) return;
          if (e.key !== "Enter" && e.key !== " ") return;
          e.preventDefault();
          onSelectSession(session);
        }}
        className={cn(
          // Fixed height so nothing (hover actions, confirm prompts) can change
          // the row size and make the list jump. Content is vertically centered.
          "group/row relative flex h-14 min-w-0 cursor-default items-center gap-3 rounded-md px-2 outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring",
          isCurrent ? "bg-accent" : "hover:bg-accent/50",
          isConfirmingAction && "bg-destructive/5 hover:bg-destructive/5",
        )}
      >
        {/* Thin ring keeps photo + fallback avatars reading as the same circle
            (the fallback's faint bg otherwise looks smaller). */}
        {agent ? (
          <ActorAvatar actorType="agent" actorId={agent.id} size={36} enableHoverCard className="ring-1 ring-inset ring-border" />
        ) : (
          <span className="size-9 shrink-0" />
        )}

        <div className="min-w-0 flex-1">
          {/* Line 1: name + time (time stays put; hover actions overlay below) */}
          <div className="flex items-center gap-1.5">
            {session.pinned && (
              <Pin
                aria-label={t(($) => $.list.pinned)}
                className="size-3 shrink-0 -rotate-45 fill-current text-muted-foreground"
              />
            )}
            <span className={cn("min-w-0 flex-1 truncate text-sm", unread > 0 ? "font-semibold text-foreground" : "font-medium")}>
              {titleText}
            </span>
            <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">{timeText}</span>
          </div>

          {/* Line 2: preview + unread badge, or an inline confirm prompt */}
          <div className="mt-0.5 flex items-center gap-2">
            {isConfirmingDelete ? (
                <ConfirmRow
                  label={t(($) => $.session_history.delete_dialog.title)}
                  cancelText={t(($) => $.session_history.delete_dialog.cancel)}
                  confirmText={
                    deleteSession.isPending
                      ? t(($) => $.session_history.delete_dialog.confirming)
                      : t(($) => $.session_history.delete_dialog.confirm)
                  }
                  pending={deleteSession.isPending}
                  onCancel={() => setConfirmingDeleteId(null)}
                  onConfirm={() => handleConfirmDelete(session)}
                />
              ) : isConfirmingStop && pendingTask ? (
                <ConfirmRow
                  label={t(($) => $.session_history.stop_dialog.title)}
                  cancelText={t(($) => $.session_history.stop_dialog.cancel)}
                  confirmText={
                    stoppingTaskId === pendingTask.task_id
                      ? t(($) => $.session_history.stop_dialog.confirming)
                      : t(($) => $.session_history.stop_dialog.confirm)
                  }
                  pending={stoppingTaskId === pendingTask.task_id}
                  onCancel={() => setConfirmingStopId(null)}
                  onConfirm={() => handleConfirmStop(session, pendingTask)}
                />
              ) : (
                <>
                  <div className="min-w-0 flex-1 overflow-hidden text-xs">{previewNode}</div>
                  {unread > 0 && (
                    <span
                      aria-label={t(($) => $.session_history.row_subtitle.new_reply)}
                      // Softer, warmer red than the vivid `destructive` token —
                      // an IM unread badge, not an error.
                      className="inline-flex h-[18px] min-w-[18px] shrink-0 items-center justify-center rounded-full bg-[oklch(0.62_0.14_18)] px-1 text-[10.5px] font-semibold text-white"
                    >
                      {unread > 99 ? "99+" : unread}
                    </span>
                  )}
                </>
              )}
            </div>
        </div>

        {/* Hover actions — absolutely positioned so showing/hiding them never
            changes the row height (which was making the list jump). The archived
            view is the only place hard-delete lives; the history view offers the
            reversible archive instead. */}
        {!isConfirmingAction && (
          <div className="absolute inset-y-0 right-1 hidden items-center gap-0.5 rounded-md bg-gradient-to-l from-accent from-40% to-transparent pl-10 pr-1 group-hover/row:flex">
            {view === "archived" ? (
              <>
                <RowAction
                  icon={<ArchiveRestore className="size-3.5" />}
                  label={t(($) => $.list.unarchive)}
                  onClick={() => setArchived.mutate({ sessionId: session.id, archived: false })}
                />
                <RowAction
                  icon={<Trash2 className="size-3.5" />}
                  label={t(($) => $.session_history.row_delete_aria)}
                  danger
                  onClick={() => setConfirmingDeleteId(session.id)}
                />
              </>
            ) : (
              <>
                <RowAction
                  icon={session.pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5 -rotate-45" />}
                  label={session.pinned ? t(($) => $.list.unpin) : t(($) => $.list.pin)}
                  onClick={() => setPinned.mutate({ sessionId: session.id, pinned: !session.pinned })}
                />
                {isRunning ? (
                  <RowAction
                    icon={<Square className="size-3 fill-current" />}
                    label={t(($) => $.session_history.row_stop_aria)}
                    danger
                    onClick={() => setConfirmingStopId(session.id)}
                  />
                ) : (
                  <RowAction
                    icon={<Archive className="size-3.5" />}
                    label={t(($) => $.list.archive)}
                    onClick={() => setArchived.mutate({ sessionId: session.id, archived: true })}
                  />
                )}
              </>
            )}
          </div>
        )}
      </div>
    );
  };

  // Archived view: a back header, then the archived rows. Delete lives only
  // here (via each row's hover actions).
  if (view === "archived") {
    return (
      <>
        <button
          type="button"
          onClick={() => setView("history")}
          className="flex w-full items-center gap-1.5 rounded-md px-2 py-2 text-left text-xs font-medium text-muted-foreground outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          <ChevronLeft className="size-4 shrink-0" />
          <span className="truncate">{t(($) => $.list.archived_title)}</span>
          <span className="ml-auto shrink-0 tabular-nums text-muted-foreground/70">
            {archivedSessions.length}
          </span>
        </button>
        {archivedSessions.map(renderRow)}
      </>
    );
  }

  // History (default) view: active rows + a footer entry into the archive.
  const archivedEntry = archivedSessions.length > 0 && (
    <button
      type="button"
      onClick={() => setView("archived")}
      className="mt-1 flex h-10 w-full items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
    >
      <span className="flex size-9 shrink-0 items-center justify-center">
        <Archive className="size-4" />
      </span>
      <span className="min-w-0 flex-1 truncate font-medium">{t(($) => $.list.archived_title)}</span>
      <span className="shrink-0 tabular-nums text-muted-foreground/70">{archivedSessions.length}</span>
      <ChevronRight className="size-4 shrink-0 text-muted-foreground/50" />
    </button>
  );

  if (historySessions.length === 0) {
    return (
      <>
        <div className="px-2 py-1.5 text-xs text-muted-foreground">
          {t(($) => $.window.no_previous)}
        </div>
        {archivedEntry}
      </>
    );
  }

  return (
    <>
      {historySessions.map(renderRow)}
      {archivedEntry}
    </>
  );
}

function RowAction({
  icon,
  label,
  onClick,
  danger,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onPointerDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
      }}
      onClick={(e) => {
        e.stopPropagation();
        e.preventDefault();
        onClick();
      }}
      className={cn(
        "inline-flex size-7 items-center justify-center rounded text-muted-foreground transition-colors focus-visible:outline-none",
        danger
          ? "hover:bg-destructive/10 hover:text-destructive focus-visible:bg-destructive/10 focus-visible:text-destructive"
          : "hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground",
      )}
    >
      {icon}
    </button>
  );
}

function ConfirmRow({
  label,
  cancelText,
  confirmText,
  pending,
  onCancel,
  onConfirm,
}: {
  label: string;
  cancelText: string;
  confirmText: string;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2">
      <span className="min-w-0 flex-1 truncate text-xs font-medium text-destructive">{label}</span>
      <div className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          onPointerDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
            onCancel();
          }}
          disabled={pending}
          className="inline-flex h-6 items-center rounded px-2 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50"
        >
          {cancelText}
        </button>
        <button
          type="button"
          onPointerDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
            onConfirm();
          }}
          disabled={pending}
          className="inline-flex h-6 items-center rounded px-2 text-[11px] font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
        >
          {confirmText}
        </button>
      </div>
    </div>
  );
}

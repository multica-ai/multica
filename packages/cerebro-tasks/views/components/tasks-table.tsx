"use client";

import { useNavigation } from "@multica/views/navigation";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import type { ColumnId, CerebroTask } from "../../core/types";

interface TasksTableProps {
  tasks: CerebroTask[];
  isLoading: boolean;
  isError: boolean;
  errorMessage?: string;
  workspaceSlug: string;
  visibleColumns: Record<ColumnId, boolean>;
}

export function TasksTable({
  tasks,
  isLoading,
  isError,
  errorMessage,
  workspaceSlug,
  visibleColumns,
}: TasksTableProps) {
  if (isError) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-6 text-sm text-destructive">
        Kunne ikke hente tasks: {errorMessage ?? "ukendt fejl"}
      </div>
    );
  }

  if (isLoading && tasks.length === 0) {
    return (
      <div className="space-y-2 rounded-md border p-3">
        {[0, 1, 2, 3, 4, 5, 6, 7].map((i) => (
          <div key={i} className="h-8 animate-pulse rounded bg-muted" />
        ))}
      </div>
    );
  }

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col items-center gap-1 rounded-md border p-12 text-center">
        <p className="text-sm font-medium">Ingen tasks matcher dine filtre</p>
        <p className="text-xs text-muted-foreground">
          Prøv at nulstille filtre eller vælge en bredere tidsperiode.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-xs">
        <thead className="bg-muted/40 text-[10px] uppercase tracking-wide text-muted-foreground">
          <tr>
            {visibleColumns.agent && <th className="px-3 py-2 text-left font-medium">Agent</th>}
            {visibleColumns.task && <th className="px-3 py-2 text-left font-medium">Task</th>}
            {visibleColumns.issue_name && <th className="px-3 py-2 text-left font-medium">Issue navn</th>}
            {visibleColumns.issue_id && <th className="px-3 py-2 text-left font-medium">Issue ID</th>}
            {visibleColumns.status && <th className="px-3 py-2 text-left font-medium">Status</th>}
            {visibleColumns.started && <th className="px-3 py-2 text-left font-medium">Started</th>}
            {visibleColumns.duration && <th className="px-3 py-2 text-left font-medium">Varighed</th>}
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <Row key={t.task_id} task={t} workspaceSlug={workspaceSlug} visibleColumns={visibleColumns} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Row({
  task,
  workspaceSlug,
  visibleColumns,
}: {
  task: CerebroTask;
  workspaceSlug: string;
  visibleColumns: Record<ColumnId, boolean>;
}) {
  const { push } = useNavigation();
  const target = rowTarget(task, workspaceSlug);
  const onClick = () => {
    if (target) push(target);
  };

  const startedISO = task.started_at ?? task.dispatched_at ?? task.created_at;
  const endedISO = task.completed_at;
  const taskTitle =
    task.task_title ||
    task.issue_title ||
    (task.chat_session_id ? "Chat task" : "Uden titel");

  return (
    <tr
      onClick={onClick}
      className={cn(
        "border-t transition-colors",
        target ? "cursor-pointer hover:bg-accent/40" : "opacity-70",
      )}
    >
      {visibleColumns.agent && (
        <td className="px-3 py-2">
          <div className="flex items-center gap-2">
            <ActorAvatar
              name={task.agent_name}
              initials={task.agent_name.charAt(0).toUpperCase()}
              avatarUrl={task.agent_avatar_url}
              size={20}
            />
            <span className="truncate font-medium">{task.agent_name}</span>
          </div>
        </td>
      )}
      {visibleColumns.task && (
        <td className="px-3 py-2">
          <div className="flex items-center gap-2">
            <TaskKindBadge isChat={!!task.chat_session_id} />
            <span className="truncate">{taskTitle}</span>
          </div>
        </td>
      )}
      {visibleColumns.issue_name && (
        <td className="px-3 py-2 text-muted-foreground">
          <span className="truncate">{task.issue_title ?? "—"}</span>
        </td>
      )}
      {visibleColumns.issue_id && (
        <td className="px-3 py-2 text-muted-foreground">
          {task.issue_number !== undefined && task.issue_number > 0 ? (
            <span className="shrink-0 rounded-sm bg-muted px-1 py-0.5 text-[10px] font-medium text-muted-foreground">
              #{task.issue_number}
            </span>
          ) : (
            "—"
          )}
        </td>
      )}
      {visibleColumns.status && (
        <td className="px-3 py-2">
          <StatusBadge status={task.status} />
        </td>
      )}
      {visibleColumns.started && (
        <td className="px-3 py-2 text-muted-foreground">{formatAbsolute(startedISO)}</td>
      )}
      {visibleColumns.duration && (
        <td className="px-3 py-2 text-muted-foreground">
          {formatDuration(startedISO, endedISO)}
        </td>
      )}
    </tr>
  );
}

function rowTarget(task: CerebroTask, workspaceSlug: string): string | null {
  if (task.chat_session_id) return `/${workspaceSlug}/chats/${task.chat_session_id}`;
  if (task.issue_id) return `/${workspaceSlug}/issues/${task.issue_id}`;
  return null;
}

function StatusBadge({ status }: { status: string }) {
  const tone =
    status === "running" || status === "dispatched"
      ? "bg-blue-500/10 text-blue-600 dark:text-blue-400"
      : status === "completed"
        ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
        : status === "failed"
          ? "bg-destructive/10 text-destructive"
          : status === "cancelled"
            ? "bg-muted text-muted-foreground"
            : "bg-amber-500/10 text-amber-600 dark:text-amber-400";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide",
        tone,
      )}
    >
      {status}
    </span>
  );
}

function TaskKindBadge({ isChat }: { isChat: boolean }) {
  return (
    <span
      className={cn(
        "shrink-0 rounded-sm px-1 py-0.5 text-[9px] font-medium uppercase tracking-wide",
        isChat
          ? "bg-violet-500/10 text-violet-600 dark:text-violet-400"
          : "bg-muted text-muted-foreground",
      )}
    >
      {isChat ? "Chat" : "Issue"}
    </span>
  );
}

function formatAbsolute(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return d.toLocaleString("da-DK", {
      day: "2-digit",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "—";
  }
}

function formatDuration(startISO: string | undefined, endISO: string | undefined): string {
  if (!startISO) return "—";
  try {
    const start = new Date(startISO).getTime();
    const end = endISO ? new Date(endISO).getTime() : Date.now();
    const seconds = Math.max(0, Math.floor((end - start) / 1000));
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    return `${h}h ${m}m`;
  } catch {
    return "—";
  }
}

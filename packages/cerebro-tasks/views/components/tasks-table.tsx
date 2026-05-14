"use client";

import { useNavigation } from "@multica/views/navigation";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import type { ColumnId, CerebroTask, GroupBy } from "../../core/types";

interface TasksTableProps {
  tasks: CerebroTask[];
  isLoading: boolean;
  isError: boolean;
  errorMessage?: string;
  workspaceSlug: string;
  visibleColumns: Record<ColumnId, boolean>;
  groupBy: GroupBy;
}

export function TasksTable({
  tasks,
  isLoading,
  isError,
  errorMessage,
  workspaceSlug,
  visibleColumns,
  groupBy,
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

  const groups = bucketize(tasks, groupBy);

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-xs">
        <thead className="bg-muted/40 text-[10px] uppercase tracking-wide text-muted-foreground">
          <tr>
            {visibleColumns.agent && <th className="px-3 py-2 text-left font-medium">Agent</th>}
            {visibleColumns.task && <th className="px-3 py-2 text-left font-medium">Task</th>}
            {visibleColumns.issue_name && <th className="px-3 py-2 text-left font-medium">Issue navn</th>}
            {visibleColumns.issue_id && <th className="px-3 py-2 text-left font-medium">Issue ID</th>}
            {visibleColumns.parent_issue && <th className="px-3 py-2 text-left font-medium">Parent issue</th>}
            {visibleColumns.project && <th className="px-3 py-2 text-left font-medium">Projekt</th>}
            {visibleColumns.status && <th className="px-3 py-2 text-left font-medium">Status</th>}
            {visibleColumns.started && <th className="px-3 py-2 text-left font-medium">Started</th>}
            {visibleColumns.duration && <th className="px-3 py-2 text-left font-medium">Varighed</th>}
            {visibleColumns.cost && <th className="px-3 py-2 text-right font-medium">Kost</th>}
            {visibleColumns.triggered_by && <th className="px-3 py-2 text-left font-medium">Startet af</th>}
          </tr>
        </thead>
        <tbody>
          {groups.map((group) => (
            <>
              {groupBy !== "none" && (
                <tr key={`group-${group.key}`} className="bg-muted/20">
                  <td
                    colSpan={countVisibleColumns(visibleColumns)}
                    className="px-3 py-1.5"
                  >
                    <GroupHeader group={group} groupBy={groupBy} />
                  </td>
                </tr>
              )}
              {group.tasks.map((t) => (
                <Row
                  key={t.task_id}
                  task={t}
                  workspaceSlug={workspaceSlug}
                  visibleColumns={visibleColumns}
                />
              ))}
            </>
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface Group {
  key: string;
  label: string;
  assigneeName?: string;
  tasks: CerebroTask[];
}

function bucketize(tasks: CerebroTask[], groupBy: GroupBy): Group[] {
  if (groupBy === "none") {
    return [{ key: "all", label: "Alle", tasks }];
  }

  const map = new Map<string, Group>();

  for (const task of tasks) {
    let key: string;
    let label: string;
    let assigneeName: string | undefined;

    if (groupBy === "project") {
      key = task.project_id ?? "__none__";
      label = task.project_title ?? "Uden projekt";
      assigneeName = task.project_lead_name;
    } else {
      key = task.parent_issue_id ?? "__none__";
      label = task.parent_issue_title
        ? task.parent_issue_number
          ? `#${task.parent_issue_number} ${task.parent_issue_title}`
          : task.parent_issue_title
        : "Uden parent issue";
      assigneeName = task.parent_issue_assignee_name;
    }

    if (!map.has(key)) {
      map.set(key, { key, label, assigneeName, tasks: [] });
    }
    map.get(key)!.tasks.push(task);
  }

  return Array.from(map.values());
}

function countVisibleColumns(visibleColumns: Record<ColumnId, boolean>): number {
  return Object.values(visibleColumns).filter(Boolean).length;
}

function GroupHeader({ group, groupBy }: { group: Group; groupBy: GroupBy }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {groupBy === "project" ? "Projekt" : "Parent issue"}
      </span>
      <span className="font-medium text-foreground">{group.label}</span>
      {group.assigneeName && (
        <>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">{group.assigneeName}</span>
        </>
      )}
      <span className="ml-1 rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
        {group.tasks.length}
      </span>
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
      {visibleColumns.parent_issue && (
        <td className="px-3 py-2 text-muted-foreground">
          <div className="flex flex-col gap-0.5">
            {task.parent_issue_title ? (
              <>
                <span className="truncate">
                  {task.parent_issue_number ? (
                    <span className="mr-1 rounded-sm bg-muted px-1 py-0.5 text-[10px] font-medium">
                      #{task.parent_issue_number}
                    </span>
                  ) : null}
                  {task.parent_issue_title}
                </span>
                {task.parent_issue_assignee_name && (
                  <span className="text-[10px] text-muted-foreground/70">
                    {task.parent_issue_assignee_name}
                  </span>
                )}
              </>
            ) : (
              "—"
            )}
          </div>
        </td>
      )}
      {visibleColumns.project && (
        <td className="px-3 py-2 text-muted-foreground">
          <div className="flex flex-col gap-0.5">
            {task.project_title ? (
              <>
                <span className="truncate">{task.project_title}</span>
                {task.project_lead_name && (
                  <span className="text-[10px] text-muted-foreground/70">
                    {task.project_lead_name}
                  </span>
                )}
              </>
            ) : (
              "—"
            )}
          </div>
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
      {visibleColumns.cost && (
        <td className="px-3 py-2 text-right text-muted-foreground">
          {formatCost(task.cost_cents)}
        </td>
      )}
      {visibleColumns.triggered_by && (
        <td className="px-3 py-2 text-muted-foreground">
          {task.triggered_by_name ?? "—"}
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

function formatCost(cents: number | undefined): string {
  if (cents === undefined || cents === 0) return "—";
  if (cents < 100) return `$0.${String(cents).padStart(2, "0")}`;
  const dollars = cents / 100;
  return `$${dollars.toFixed(2)}`;
}

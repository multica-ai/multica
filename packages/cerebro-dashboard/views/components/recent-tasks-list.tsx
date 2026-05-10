"use client";

import { useNavigation } from "@multica/views/navigation";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import type { DashboardOverview, RecentTask } from "../../core/api";

interface RecentTasksListProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  workspaceSlug: string;
}

// Recent agent tasks across the workspace, joined with agent names + issue
// titles so each row reads like a sentence rather than a UUID.
export function RecentTasksList({
  data,
  isLoading,
  workspaceSlug,
}: RecentTasksListProps) {
  const tasks = data?.recent_tasks ?? [];

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Recent tasks
        </h3>
        {tasks.length > 0 && (
          <span className="text-[11px] text-muted-foreground">{tasks.length}</span>
        )}
      </div>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="space-y-2 p-3">
            {[0, 1, 2, 3, 4].map((i) => (
              <div key={i} className="h-6 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : tasks.length === 0 ? (
          <p className="p-6 text-center text-sm text-muted-foreground">
            Ingen tasks endnu.
          </p>
        ) : (
          <ul className="divide-y">
            {tasks.map((t) => (
              <Row key={t.task_id} task={t} workspaceSlug={workspaceSlug} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Row({ task, workspaceSlug }: { task: RecentTask; workspaceSlug: string }) {
  const { push } = useNavigation();
  const onClick = () => {
    if (task.issue_id) push(`/${workspaceSlug}/issues/${task.issue_id}`);
  };
  const ts = task.completed_at ?? task.started_at ?? task.created_at;
  const title = task.task_title || task.issue_title || (task.chat_session_id ? "Chat task" : "ingen issue");

  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        disabled={!task.issue_id}
        className={cn(
          "flex w-full items-center gap-2 px-3 py-2 text-left text-xs",
          task.issue_id && "hover:bg-accent/40",
        )}
      >
        <StatusDot status={task.status} />
        <ActorAvatar
          name={task.agent_name}
          initials={task.agent_name.charAt(0).toUpperCase()}
          avatarUrl={task.agent_avatar_url}
          size={16}
        />
        <span className="font-medium">{task.agent_name}</span>
        <span className="text-muted-foreground">·</span>
        <span className="truncate text-muted-foreground">
          {title}
        </span>
        <span className="ml-auto shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">
          {task.status}
        </span>
        <span className="shrink-0 text-[10px] text-muted-foreground">
          {relativeTime(ts)}
        </span>
      </button>
    </li>
  );
}

function StatusDot({ status }: { status: string }) {
  const tone =
    status === "running" || status === "dispatched"
      ? "bg-blue-500"
      : status === "completed"
        ? "bg-emerald-500"
        : status === "failed"
          ? "bg-destructive"
          : status === "cancelled"
            ? "bg-muted-foreground"
            : "bg-amber-500";
  return <span className={cn("size-1.5 rounded-full", tone)} aria-hidden />;
}

function relativeTime(iso: string): string {
  try {
    const d = new Date(iso);
    const seconds = Math.floor((Date.now() - d.getTime()) / 1000);
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
    return `${Math.floor(seconds / 86400)}d`;
  } catch {
    return "";
  }
}

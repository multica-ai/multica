"use client";

import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { api } from "@multica/core/api";
import type { Agent, AgentTask } from "@multica/core/types";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { useDashboardStore } from "../../core/store";
import { timeRangeToDays } from "../../core/types";

interface RecentTasksListProps {
  wsId: string;
}

const EMPTY_AGENTS: Agent[] = [];

export function RecentTasksList({ wsId }: RecentTasksListProps) {
  const range = useDashboardStore((s) => s.range);
  const { data: snapshot = [], isLoading } = useQuery(
    agentTaskSnapshotOptions(wsId),
  );
  const { data: agents = EMPTY_AGENTS } = useQuery({
    queryKey: ["workspaces", wsId, "agents", "list"],
    queryFn: () => api.listAgents({ workspace_id: wsId }),
    enabled: !!wsId,
    staleTime: 60 * 1000,
  });

  const cutoff = Date.now() - timeRangeToDays(range) * 24 * 60 * 60 * 1000;
  const tasks = snapshot
    .filter((t) => {
      const ts = t.completed_at ?? t.started_at ?? t.dispatched_at;
      if (!ts) return t.status === "queued" || t.status === "running";
      return new Date(ts).getTime() >= cutoff;
    })
    .sort((a, b) => {
      const at = a.completed_at ?? a.started_at ?? a.dispatched_at ?? "";
      const bt = b.completed_at ?? b.started_at ?? b.dispatched_at ?? "";
      return bt.localeCompare(at);
    })
    .slice(0, 12);

  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);
  const agentForId = (id: string) => agents.find((a) => a.id === id) ?? null;

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Recent tasks
        </h3>
        <span className="text-[11px] text-muted-foreground">{range}</span>
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
            Ingen tasks i den valgte periode.
          </p>
        ) : (
          <ul className="divide-y">
            {tasks.map((task) => {
              const agent = agentForId(task.agent_id);
              return (
                <li
                  key={task.id}
                  className="flex items-center gap-2 px-3 py-2 text-xs"
                >
                  <StatusDot status={task.status} />
                  {agent && (
                    <ActorAvatar
                      name={agent.name}
                      initials={agent.name.charAt(0).toUpperCase()}
                      avatarUrl={agent.avatar_url}
                      size={16}
                    />
                  )}
                  <span className="font-medium">{agentName(task.agent_id)}</span>
                  <span className="text-muted-foreground">·</span>
                  <span className="truncate text-muted-foreground">
                    {task.issue_id ? `task ${task.id.slice(0, 8)}` : "no linked issue"}
                  </span>
                  <span className="ml-auto shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">
                    {task.status}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function StatusDot({ status }: { status: AgentTask["status"] }) {
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

"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import type { Agent, AgentTask } from "@multica/core/types";

const EMPTY_AGENTS: Agent[] = [];

interface AgentStripProps {
  wsId: string;
}

// Each card: agent name + last terminal task title + status dot. Reads from
// the shared agent-task-snapshot cache so it doesn't add network traffic on
// top of pages that already mounted that query.
export function AgentStrip({ wsId }: AgentStripProps) {
  const { data: agents = EMPTY_AGENTS, isLoading } = useQuery({
    queryKey: ["workspaces", wsId, "agents", "list"],
    queryFn: () => api.listAgents({ workspace_id: wsId }),
    enabled: !!wsId,
    staleTime: 60 * 1000,
  });
  const { data: snapshot } = useQuery(agentTaskSnapshotOptions(wsId));

  if (isLoading) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Card key={i} className="h-28 animate-pulse" />
        ))}
      </div>
    );
  }

  if (agents.length === 0) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-sm text-muted-foreground">
          Ingen agenter i dette workspace endnu.
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {agents.slice(0, 8).map((agent) => {
        const lastTask = pickLatestTask(snapshot, agent.id);
        return <AgentCard key={agent.id} agent={agent} lastTask={lastTask} />;
      })}
    </div>
  );
}

function pickLatestTask(
  snapshot: AgentTask[] | undefined,
  agentId: string,
): AgentTask | null {
  if (!snapshot) return null;
  return (
    snapshot
      .filter((t) => t.agent_id === agentId)
      .sort((a, b) => {
        const at = a.completed_at ?? a.started_at ?? a.dispatched_at ?? "";
        const bt = b.completed_at ?? b.started_at ?? b.dispatched_at ?? "";
        return bt.localeCompare(at);
      })[0] ?? null
  );
}

function AgentCard({ agent, lastTask }: { agent: Agent; lastTask: AgentTask | null }) {
  return (
    <Card>
      <CardContent className="space-y-2 p-3">
        <div className="flex items-center gap-2">
          <ActorAvatar
            name={agent.name}
            initials={agent.name.charAt(0).toUpperCase()}
            avatarUrl={agent.avatar_url}
            size={20}
          />
          <span className="truncate text-sm font-medium">{agent.name}</span>
        </div>
        {lastTask ? (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <StatusDot status={lastTask.status} />
              <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
                {lastTask.status}
              </span>
            </div>
            <p className="line-clamp-1 text-xs text-muted-foreground">
              {lastTask.issue_id ? `task ${lastTask.id.slice(0, 8)}` : "no linked issue"}
            </p>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">Ingen kørsler endnu.</p>
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

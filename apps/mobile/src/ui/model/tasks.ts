import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import type { AgentTask } from "@multica/core/types";

export type RunPhase = "running" | "queued" | "failed" | "done";

export function runPhase(status: AgentTask["status"]): RunPhase {
  switch (status) {
    case "running":
      return "running";
    case "queued":
    case "dispatched":
    case "waiting_local_directory":
    case "deferred":
      return "queued";
    case "failed":
      return "failed";
    default:
      return "done";
  }
}

export function isActiveTask(task: Pick<AgentTask, "status">): boolean {
  const phase = runPhase(task.status);
  return phase === "running" || phase === "queued";
}

/** Every unfinished run in the workspace, newest first. */
export function useActiveTasks(wsId: string): { tasks: AgentTask[]; isPending: boolean; isError: boolean; refetch: () => void } {
  const query = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    select: (tasks: AgentTask[]) =>
      tasks.filter(isActiveTask).sort((a, b) => (b.started_at ?? b.created_at).localeCompare(a.started_at ?? a.created_at)),
  });
  return { tasks: query.data ?? [], isPending: query.isPending, isError: query.isError, refetch: () => void query.refetch() };
}

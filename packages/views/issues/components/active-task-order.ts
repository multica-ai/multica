import type { AgentTask } from "@multica/core/types";

type OrderedTask = Pick<AgentTask, "id" | "status" | "priority" | "created_at">;

function phase(task: OrderedTask): number {
  if (task.status === "running") return 0;
  if (task.status === "dispatched" || task.status === "waiting_local_directory") return 1;
  return 2;
}

/** Show current execution first, then queued work in ClaimAgentTask order. */
export function compareActiveIssueTasks(a: OrderedTask, b: OrderedTask): number {
  const phaseDiff = phase(a) - phase(b);
  if (phaseDiff) return phaseDiff;
  const priorityDiff = (b.priority ?? 0) - (a.priority ?? 0);
  if (priorityDiff) return priorityDiff;
  // Queue age belongs to the run, not its latest coalesced comment. Parse
  // timestamps so equivalent instants with different offsets sort identically.
  const ageDiff = Date.parse(a.created_at) - Date.parse(b.created_at);
  return (Number.isFinite(ageDiff) ? ageDiff : 0) || a.id.localeCompare(b.id);
}

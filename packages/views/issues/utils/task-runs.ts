import type { AgentTask } from "@multica/core/types";

function timeValue(value: string | null | undefined): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function compareTaskRunsByCreatedAtAsc(a: AgentTask, b: AgentTask): number {
  const diff = timeValue(a.created_at) - timeValue(b.created_at);
  if (diff !== 0) return diff;
  return a.id.localeCompare(b.id);
}

export function compareTaskRunsByCreatedAtDesc(a: AgentTask, b: AgentTask): number {
  const diff = timeValue(b.created_at) - timeValue(a.created_at);
  if (diff !== 0) return diff;
  return b.id.localeCompare(a.id);
}

export function sortTaskRunsByCreatedAtAsc(tasks: readonly AgentTask[]): AgentTask[] {
  return [...tasks].sort(compareTaskRunsByCreatedAtAsc);
}

export function sortTaskRunsByCreatedAtDesc(tasks: readonly AgentTask[]): AgentTask[] {
  return [...tasks].sort(compareTaskRunsByCreatedAtDesc);
}

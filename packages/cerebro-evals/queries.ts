import { queryOptions } from "@tanstack/react-query";
import { fetchEvalBindings, fetchEvalRuns, fetchEvalSchedule, fetchEvals } from "./api";

export const evalKeys = {
  all: (workspaceId: string) => ["cerebro", "evals", workspaceId] as const,
  list: (workspaceId: string) => [...evalKeys.all(workspaceId), "list"] as const,
  runs: (workspaceId: string, evalId: string) => [...evalKeys.all(workspaceId), "runs", evalId] as const,
  bindings: (workspaceId: string) => [...evalKeys.all(workspaceId), "bindings"] as const,
  schedule: (workspaceId: string, evalId: string) => [...evalKeys.all(workspaceId), "schedule", evalId] as const,
};

export function evalsListOptions(workspaceId: string) {
  return queryOptions({ queryKey: evalKeys.list(workspaceId), queryFn: fetchEvals, enabled: !!workspaceId });
}

export function evalScheduleOptions(workspaceId: string, evalId: string) {
  return queryOptions({ queryKey: evalKeys.schedule(workspaceId, evalId), queryFn: () => fetchEvalSchedule(evalId), enabled: !!workspaceId && !!evalId });
}

export function evalRunsOptions(workspaceId: string, evalId: string) {
  return queryOptions({ queryKey: evalKeys.runs(workspaceId, evalId), queryFn: () => fetchEvalRuns(evalId), enabled: !!workspaceId && !!evalId });
}

export function evalBindingsOptions(workspaceId: string) {
  return queryOptions({ queryKey: evalKeys.bindings(workspaceId), queryFn: () => fetchEvalBindings(), enabled: !!workspaceId });
}

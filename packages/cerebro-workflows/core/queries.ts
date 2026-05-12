import { queryOptions } from "@tanstack/react-query";
import { fetchWorkflow, fetchWorkflowRuns, fetchWorkflows } from "./api";

export const cerebroWorkflowsKeys = {
  all: (wsId: string) => ["cerebro", "workflows", wsId] as const,
  list: (wsId: string) => [...cerebroWorkflowsKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...cerebroWorkflowsKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, workflowId: string | null, limit: number, offset: number) =>
    [...cerebroWorkflowsKeys.all(wsId), "runs", workflowId ?? "all", limit, offset] as const,
};

export function cerebroWorkflowsListOptions(wsId: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.list(wsId),
    queryFn: fetchWorkflows,
    enabled: !!wsId,
    staleTime: 15 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function cerebroWorkflowDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.detail(wsId, id),
    queryFn: () => fetchWorkflow(id),
    enabled: !!wsId && !!id,
    staleTime: 30 * 1000,
  });
}

export function cerebroWorkflowRunsOptions(
  wsId: string,
  workflowId: string | null,
  limit = 50,
  offset = 0,
) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.runs(wsId, workflowId, limit, offset),
    queryFn: () => fetchWorkflowRuns({ workflowId, limit, offset }),
    enabled: !!wsId,
    staleTime: 5 * 1000,
    refetchInterval: 10 * 1000,
    placeholderData: (prev) => prev,
  });
}

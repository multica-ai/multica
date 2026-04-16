import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const workflowKeys = {
  all: (wsId: string) => ["workflows", wsId] as const,
  list: (wsId: string) => [...workflowKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...workflowKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, workflowId: string) =>
    [...workflowKeys.all(wsId), "runs", workflowId] as const,
  runDetail: (wsId: string, runId: string) =>
    [...workflowKeys.all(wsId), "run-detail", runId] as const,
  pendingApprovals: (wsId: string) =>
    [...workflowKeys.all(wsId), "pending-approvals"] as const,
};

export function workflowListOptions(wsId: string) {
  return queryOptions({
    queryKey: workflowKeys.list(wsId),
    queryFn: () => api.listWorkflows(),
    select: (data) => data.workflows,
  });
}

export function workflowDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(wsId, id),
    queryFn: () => api.getWorkflow(id),
  });
}

export function workflowRunsOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.runs(wsId, workflowId),
    queryFn: () => api.listWorkflowRuns(workflowId),
    select: (data) => data.runs,
  });
}

export function workflowRunDetailOptions(wsId: string, runId: string) {
  return queryOptions({
    queryKey: workflowKeys.runDetail(wsId, runId),
    queryFn: () => api.getWorkflowRun(runId),
  });
}

export function pendingApprovalsOptions(wsId: string) {
  return queryOptions({
    queryKey: workflowKeys.pendingApprovals(wsId),
    queryFn: () => api.listPendingApprovals(),
  });
}

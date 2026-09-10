import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueWorkflowKeys = {
  all: (wsId: string) => ["issue-workflows", wsId] as const,
  detail: (wsId: string, workflowId: string) =>
    [...issueWorkflowKeys.all(wsId), "detail", workflowId] as const,
  executionsAll: (wsId: string) => [...issueWorkflowKeys.all(wsId), "executions"] as const,
  executions: (wsId: string, issueId: string) =>
    [...issueWorkflowKeys.all(wsId), "executions", issueId] as const,
  effective: (wsId: string, projectId: string | null) =>
    [...issueWorkflowKeys.all(wsId), "effective", projectId] as const,
};

export function issueWorkflowOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: issueWorkflowKeys.detail(wsId, workflowId),
    queryFn: () => api.getIssueWorkflow(workflowId),
    enabled: workflowId.length > 0,
    staleTime: 5 * 60_000,
  });
}

export function issueAutomationExecutionsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueWorkflowKeys.executions(wsId, issueId),
    queryFn: () => api.listIssueAutomationExecutions(issueId),
  });
}

/**
 * The workflow a newly-created issue will bind to. Existing issues use their
 * own pinned workflow_id and must not be resolved through this query.
 */
export function effectiveIssueWorkflowOptions(
  wsId: string,
  projectId: string | null,
  includeArchived = false,
) {
  return queryOptions({
    queryKey: [...issueWorkflowKeys.effective(wsId, projectId), { includeArchived }] as const,
    queryFn: () => api.getEffectiveIssueWorkflow(projectId, includeArchived),
    staleTime: 5 * 60_000,
  });
}

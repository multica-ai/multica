import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const workflowKeys = {
  all: (wsId: string) => ["workflows", wsId] as const,
  list: (wsId: string) => [...workflowKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...workflowKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, id: string) => [...workflowKeys.all(wsId), "runs", id] as const,
  run: (wsId: string, id: string, runId: string) => [...workflowKeys.runs(wsId, id), runId] as const,
  node: (wsId: string, id: string, runId: string, nodeIdOrActivationId: string) => [...workflowKeys.run(wsId, id, runId), "node", nodeIdOrActivationId] as const,
  events: (wsId: string, id: string, runId: string) => [...workflowKeys.run(wsId, id, runId), "events"] as const,
  templates: (wsId: string) => [...workflowKeys.all(wsId), "templates"] as const,
  workItems: (wsId: string, status?: string) => [...workflowKeys.all(wsId), "work-items", status ?? "all"] as const,
  workItem: (wsId: string, workflowId: string, runId: string, itemId: string) => [...workflowKeys.run(wsId, workflowId, runId), "work-item", itemId] as const,
};

export function workflowListOptions(wsId: string) {
  return queryOptions({ queryKey: workflowKeys.list(wsId), queryFn: () => api.listWorkflows(wsId), enabled: !!wsId, select: (data) => data.workflows });
}

export function workflowTemplateListOptions(wsId: string) {
  return queryOptions({ queryKey: workflowKeys.templates(wsId), queryFn: () => api.listWorkflowTemplates(wsId), enabled: !!wsId, select: (data) => data.templates });
}
export function workflowDetailOptions(wsId: string, id: string) {
  // No refetchInterval: realtime invalidates workflowKeys.all(wsId) on
  // workflow/workflow_run/workflow_work_item events (use-realtime-sync.ts),
  // so a steady poll here double-fetched the list on every connected client.
  return queryOptions({ queryKey: workflowKeys.detail(wsId, id), queryFn: () => api.getWorkflow(wsId, id), enabled: !!wsId && !!id });
}
export function workflowRunListOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: workflowKeys.runs(wsId, id), queryFn: () => api.listWorkflowRuns(wsId, id), enabled: !!wsId && !!id, select: (data) => data.runs });
}
export function workflowRunDetailOptions(wsId: string, id: string, runId: string) {
  // Run-scoped polls stay: they are the "watching an active run" fallback for
  // when the WS connection is down, they stop on terminal status, and only
  // the viewers with a run page open pay for them.
  return queryOptions({
    queryKey: workflowKeys.run(wsId, id, runId), queryFn: () => api.getWorkflowRun(wsId, id, runId), enabled: !!wsId && !!id && !!runId,
		refetchInterval: (query) => query.state.data && ["succeeded", "failed", "cancelled"].includes(query.state.data.status) ? false : 3_000,
  });
}
export function workflowRunEventsOptions(wsId: string, id: string, runId: string) {
  return queryOptions({
    queryKey: workflowKeys.events(wsId, id, runId),
    queryFn: () => api.listWorkflowRunEvents(wsId, id, runId, { limit: 200 }),
    enabled: !!wsId && !!id && !!runId,
    refetchInterval: 3_000,
  });
}

export function workflowRunNodeOptions(wsId: string, id: string, runId: string, nodeIdOrActivationId: string) {
  return queryOptions({
    queryKey: workflowKeys.node(wsId, id, runId, nodeIdOrActivationId),
    queryFn: () => api.getWorkflowRunNode(wsId, id, runId, nodeIdOrActivationId),
    enabled: !!wsId && !!id && !!runId && !!nodeIdOrActivationId,
  });
}

export function workflowWorkItemListOptions(wsId: string, status?: "open" | "closed" | "expired") {
  return queryOptions({
    queryKey: workflowKeys.workItems(wsId, status),
    queryFn: () => api.listWorkflowWorkItems(wsId, status),
    enabled: !!wsId,
    select: (data) => data.workItems,
    // No refetchInterval: workflow_work_item realtime events already
    // invalidate this tree; a steady 5s poll double-fetched for every
    // connected client with the inbox open.
  });
}

export function workflowWorkItemOptions(wsId: string, workflowId: string, runId: string, itemId: string) {
  return queryOptions({ queryKey: workflowKeys.workItem(wsId, workflowId, runId, itemId), queryFn: () => api.getWorkflowWorkItem(wsId, workflowId, runId, itemId), enabled: !!wsId && !!workflowId && !!runId && !!itemId });
}

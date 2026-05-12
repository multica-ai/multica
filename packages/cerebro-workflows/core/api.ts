import { api } from "@multica/core/api";
import type {
  CerebroWorkflow,
  WorkflowRunsListResponse,
  WorkflowsListResponse,
  WorkflowWriteInput,
} from "./types";

export async function fetchWorkflows(): Promise<WorkflowsListResponse> {
  return api.listCerebroWorkflows<WorkflowsListResponse>();
}

export async function fetchWorkflow(id: string): Promise<CerebroWorkflow> {
  return api.getCerebroWorkflow<CerebroWorkflow>(id);
}

export async function createWorkflow(payload: WorkflowWriteInput): Promise<CerebroWorkflow> {
  return api.createCerebroWorkflow<CerebroWorkflow>(payload);
}

export async function updateWorkflow(
  id: string,
  payload: WorkflowWriteInput,
): Promise<CerebroWorkflow> {
  return api.updateCerebroWorkflow<CerebroWorkflow>(id, payload);
}

export async function toggleWorkflow(id: string, enabled: boolean): Promise<{ enabled: boolean }> {
  return api.toggleCerebroWorkflow<{ enabled: boolean }>(id, enabled);
}

export async function deleteWorkflow(id: string): Promise<void> {
  await api.deleteCerebroWorkflow(id);
}

export async function fetchWorkflowRuns(filter: {
  workflowId?: string | null;
  limit?: number;
  offset?: number;
}): Promise<WorkflowRunsListResponse> {
  return api.listCerebroWorkflowRuns<WorkflowRunsListResponse>(filter);
}

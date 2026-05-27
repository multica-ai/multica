import { api, parseWithFallback } from "@multica/core/api";
import {
  assignmentsListSchema,
  EMPTY_ASSIGNMENTS_LIST,
  EMPTY_PROJECT_ASSIGNMENT,
  EMPTY_STATUS_MODEL,
  EMPTY_STATUS_MODELS_LIST,
  projectAssignmentSchema,
  statusModelSchema,
  statusModelsListSchema,
} from "./api-schemas";
import type {
  AssignmentsListResponse,
  CerebroStatusModel,
  ProjectStatusAssignment,
  StatusModelsListResponse,
  StatusModelWriteInput,
} from "./types";

// All reads route through parseWithFallback so an older/newer backend degrades
// to safe defaults instead of white-screening the settings page. Calls go
// through the generic cerebroRequest primitive, so the cerebro zone owns this
// REST surface without patching the upstream API client.

export async function fetchStatusModels(): Promise<StatusModelsListResponse> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/status-models");
  return parseWithFallback(raw, statusModelsListSchema, EMPTY_STATUS_MODELS_LIST, {
    endpoint: "fetchStatusModels",
  });
}

export async function fetchStatusModel(id: string): Promise<CerebroStatusModel> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/status-models/${id}`);
  return parseWithFallback(raw, statusModelSchema, EMPTY_STATUS_MODEL, {
    endpoint: "fetchStatusModel",
  });
}

export async function createStatusModel(
  payload: StatusModelWriteInput,
): Promise<CerebroStatusModel> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/status-models", {
    method: "POST",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, statusModelSchema, EMPTY_STATUS_MODEL, {
    endpoint: "createStatusModel",
  });
}

export async function updateStatusModel(
  id: string,
  payload: StatusModelWriteInput,
): Promise<CerebroStatusModel> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/status-models/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, statusModelSchema, EMPTY_STATUS_MODEL, {
    endpoint: "updateStatusModel",
  });
}

export async function deleteStatusModel(id: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/status-models/${id}`, { method: "DELETE" });
}

export async function fetchStatusModelAssignments(): Promise<AssignmentsListResponse> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/status-models/assignments");
  return parseWithFallback(raw, assignmentsListSchema, EMPTY_ASSIGNMENTS_LIST, {
    endpoint: "fetchStatusModelAssignments",
  });
}

export async function assignProjectStatusModel(
  projectId: string,
  statusModelId: string,
): Promise<ProjectStatusAssignment> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/projects/${projectId}/status-model`,
    { method: "PUT", body: JSON.stringify({ status_model_id: statusModelId }) },
  );
  return parseWithFallback(raw, projectAssignmentSchema, EMPTY_PROJECT_ASSIGNMENT, {
    endpoint: "assignProjectStatusModel",
  });
}

export async function clearProjectStatusModel(projectId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/projects/${projectId}/status-model`, {
    method: "DELETE",
  });
}

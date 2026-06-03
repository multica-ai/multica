import { api, parseWithFallback } from "@multica/core/api";
import {
  assignmentsListSchema,
  EMPTY_ASSIGNMENTS_LIST,
  EMPTY_PROJECT_ASSIGNMENT,
  EMPTY_STATUS_MODEL,
  EMPTY_STATUS_MODELS_LIST,
  issueCustomStatusSchema,
  projectAssignmentSchema,
  statusModelSchema,
  statusModelsListSchema,
} from "./api-schemas";
import type {
  AssignmentsListResponse,
  CerebroStatusModel,
  IssueCustomStatus,
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
  // Per-base admin choice from the assign modal (base_status -> custom_status_key).
  // When provided, the server pins existing project issues to the chosen custom
  // status immediately so the board matches what the admin confirmed.
  mapping?: Record<string, string>,
): Promise<ProjectStatusAssignment> {
  const body: { status_model_id: string; mapping?: Record<string, string> } = {
    status_model_id: statusModelId,
  };
  if (mapping && Object.keys(mapping).length > 0) {
    body.mapping = mapping;
  }
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/projects/${projectId}/status-model`,
    { method: "PUT", body: JSON.stringify(body) },
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

// FIR-2800: workspace default model — mark / clear the workspace default.

export async function setWorkspaceDefaultStatusModel(id: string): Promise<CerebroStatusModel> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/status-models/${id}/set-default`,
    { method: "PATCH" },
  );
  return parseWithFallback(raw, statusModelSchema, EMPTY_STATUS_MODEL, {
    endpoint: "setWorkspaceDefaultStatusModel",
  });
}

export async function clearWorkspaceDefaultStatusModel(): Promise<void> {
  await api.cerebroRequest<void>("/api/cerebro/status-models/default", { method: "DELETE" });
}

// v2b: per-issue custom status pin (FIR-1550).
//
// On 404, fetchIssueCustomStatus returns null — "this issue has no pinned
// custom status, fall back to the project's default mapping." Callers
// should treat null as a normal state, not an error.

const EMPTY_ISSUE_CUSTOM_STATUS: IssueCustomStatus = {
  issue_id: "",
  workspace_id: "",
  status_model_id: "",
  custom_status_key: "",
  base_status: "",
  set_by_id: "",
  set_by_type: "system",
  created_at: "",
  updated_at: "",
};

export async function fetchIssueCustomStatus(
  issueId: string,
): Promise<IssueCustomStatus | null> {
  try {
    const raw = await api.cerebroRequest<unknown>(
      `/api/cerebro/issues/${issueId}/custom-status`,
    );
    return parseWithFallback(raw, issueCustomStatusSchema, EMPTY_ISSUE_CUSTOM_STATUS, {
      endpoint: "fetchIssueCustomStatus",
    });
  } catch {
    return null;
  }
}

export async function setIssueCustomStatus(
  issueId: string,
  statusModelId: string,
  customStatusKey: string,
): Promise<IssueCustomStatus> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/issues/${issueId}/custom-status`,
    {
      method: "PUT",
      body: JSON.stringify({
        status_model_id: statusModelId,
        custom_status_key: customStatusKey,
      }),
    },
  );
  return parseWithFallback(raw, issueCustomStatusSchema, EMPTY_ISSUE_CUSTOM_STATUS, {
    endpoint: "setIssueCustomStatus",
  });
}

export async function clearIssueCustomStatus(issueId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/issues/${issueId}/custom-status`, {
    method: "DELETE",
  });
}

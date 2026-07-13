import { api, parseWithFallback } from "@multica/core/api";
import {
  completeSprintResponseSchema,
  EMPTY_RECURRING_TASKS_LIST,
  EMPTY_SELECTABLE_SPRINTS_LIST,
  EMPTY_SPRINT_ISSUES_LIST,
  EMPTY_SPRINTS_LIST,
  recurringTaskSchema,
  recurringTasksListSchema,
  selectableSprintsListSchema,
  sprintIssueLinkSchema,
  sprintIssuesListSchema,
  sprintSchema,
  sprintSettingsSchema,
  sprintsListSchema,
} from "./api-schemas";
import type {
  CompleteSprintInput,
  CompleteSprintResponse,
  RecurringTask,
  RecurringTasksResponse,
  RecurringTaskWriteInput,
  SelectableSprintsResponse,
  Sprint,
  SprintCreateInput,
  SprintIssueLink,
  SprintIssuesResponse,
  SprintSettings,
  SprintSettingsWriteInput,
  SprintsListResponse,
  SprintUpdateInput,
} from "./types";

// ===========================================================================
// Settings
// ===========================================================================

export async function fetchSprintSettings(projectId: string): Promise<SprintSettings | null> {
  try {
    const raw = await api.cerebroRequest<unknown>(
      `/api/cerebro/projects/${projectId}/sprint-settings`,
    );
    return parseWithFallback(raw, sprintSettingsSchema, null, {
      endpoint: "fetchSprintSettings",
    });
  } catch (err) {
    // 404 → settings have never been saved for this project. The caller
    // treats null as "feature not enabled yet".
    if (isNotFound(err)) return null;
    throw err;
  }
}

export async function upsertSprintSettings(
  projectId: string,
  payload: SprintSettingsWriteInput,
): Promise<SprintSettings | null> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/projects/${projectId}/sprint-settings`,
    {
      method: "PUT",
      body: JSON.stringify(payload),
    },
  );
  return parseWithFallback(raw, sprintSettingsSchema, null, {
    endpoint: "upsertSprintSettings",
  });
}

export async function deleteSprintSettings(projectId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/projects/${projectId}/sprint-settings`, {
    method: "DELETE",
  });
}

// ===========================================================================
// Sprints
// ===========================================================================

export async function fetchSprints(projectId: string): Promise<SprintsListResponse> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/projects/${projectId}/sprints`);
  return parseWithFallback(raw, sprintsListSchema, EMPTY_SPRINTS_LIST, {
    endpoint: "fetchSprints",
  });
}

// FIR-1657: every sprint an issue may be assigned to — its own project's
// sprints plus any opted-in project's sprints in the workspace.
export async function fetchSelectableSprints(
  issueId: string,
): Promise<SelectableSprintsResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/issues/${issueId}/selectable-sprints`,
  );
  return parseWithFallback(raw, selectableSprintsListSchema, EMPTY_SELECTABLE_SPRINTS_LIST, {
    endpoint: "fetchSelectableSprints",
  });
}

export async function createSprint(
  projectId: string,
  payload: SprintCreateInput,
): Promise<Sprint | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/projects/${projectId}/sprints`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, sprintSchema, null, { endpoint: "createSprint" });
}

export async function fetchSprint(sprintId: string): Promise<Sprint | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/sprints/${sprintId}`);
  return parseWithFallback(raw, sprintSchema, null, { endpoint: "fetchSprint" });
}

export async function updateSprint(
  sprintId: string,
  payload: SprintUpdateInput,
): Promise<Sprint | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/sprints/${sprintId}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, sprintSchema, null, { endpoint: "updateSprint" });
}

export async function deleteSprint(sprintId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/sprints/${sprintId}`, { method: "DELETE" });
}

// FIR-2828: end a sprint and choose what happens to any issue still assigned
// to it (leave it, move it to the backlog, or move it into another sprint).
export async function completeSprint(
  sprintId: string,
  payload: CompleteSprintInput,
): Promise<CompleteSprintResponse | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/sprints/${sprintId}/complete`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, completeSprintResponseSchema, null, {
    endpoint: "completeSprint",
  });
}

export async function fetchSprintIssues(sprintId: string): Promise<SprintIssuesResponse> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/sprints/${sprintId}/issues`);
  return parseWithFallback(raw, sprintIssuesListSchema, EMPTY_SPRINT_ISSUES_LIST, {
    endpoint: "fetchSprintIssues",
  });
}

// ===========================================================================
// Issue ↔ sprint assignment
// ===========================================================================

export async function fetchIssueSprintAssignment(
  issueId: string,
): Promise<SprintIssueLink | null> {
  try {
    const raw = await api.cerebroRequest<unknown>(`/api/cerebro/issues/${issueId}/sprint`);
    return parseWithFallback(raw, sprintIssueLinkSchema, null, {
      endpoint: "fetchIssueSprintAssignment",
    });
  } catch (err) {
    if (isNotFound(err)) return null;
    throw err;
  }
}

export async function assignIssueToSprint(issueId: string, sprintId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/issues/${issueId}/sprint`, {
    method: "PUT",
    body: JSON.stringify({ sprint_id: sprintId }),
  });
}

export async function removeIssueFromSprint(issueId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/issues/${issueId}/sprint`, {
    method: "PUT",
    body: JSON.stringify({ sprint_id: "" }),
  });
}

// ===========================================================================
// Recurring tasks
// ===========================================================================

export async function fetchRecurringTasks(projectId: string): Promise<RecurringTasksResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/projects/${projectId}/sprint-recurring-tasks`,
  );
  return parseWithFallback(raw, recurringTasksListSchema, EMPTY_RECURRING_TASKS_LIST, {
    endpoint: "fetchRecurringTasks",
  });
}

export async function createRecurringTask(
  projectId: string,
  payload: RecurringTaskWriteInput,
): Promise<RecurringTask | null> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/projects/${projectId}/sprint-recurring-tasks`,
    {
      method: "POST",
      body: JSON.stringify(payload),
    },
  );
  return parseWithFallback(raw, recurringTaskSchema, null, {
    endpoint: "createRecurringTask",
  });
}

export async function updateRecurringTask(
  id: string,
  payload: RecurringTaskWriteInput,
): Promise<RecurringTask | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/sprint-recurring-tasks/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, recurringTaskSchema, null, {
    endpoint: "updateRecurringTask",
  });
}

export async function deleteRecurringTask(id: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/sprint-recurring-tasks/${id}`, {
    method: "DELETE",
  });
}

function isNotFound(err: unknown): boolean {
  if (typeof err !== "object" || err === null) return false;
  const status = (err as { status?: unknown }).status;
  return status === 404;
}

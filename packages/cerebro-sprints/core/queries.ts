import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";

import {
  assignIssueToSprint,
  createRecurringTask,
  createSprint,
  deleteRecurringTask,
  deleteSprint,
  deleteSprintSettings,
  fetchIssueSprintAssignment,
  fetchRecurringTasks,
  fetchSprint,
  fetchSprintIssues,
  fetchSprintSettings,
  fetchSprints,
  removeIssueFromSprint,
  updateRecurringTask,
  updateSprint,
  upsertSprintSettings,
} from "./api";
import type {
  RecurringTaskWriteInput,
  SprintCreateInput,
  SprintSettingsWriteInput,
  SprintUpdateInput,
} from "./types";

// Query key factory — scoped by workspace so switching workspaces never
// leaks cached cross-workspace data (per CLAUDE.md → workspace-scoped queries).

export const sprintsKeys = {
  all: (wsId: string) => ["cerebro", "sprints", wsId] as const,
  settings: (wsId: string, projectId: string) =>
    [...sprintsKeys.all(wsId), "settings", projectId] as const,
  projectSprints: (wsId: string, projectId: string) =>
    [...sprintsKeys.all(wsId), "list", projectId] as const,
  sprint: (wsId: string, sprintId: string) =>
    [...sprintsKeys.all(wsId), "sprint", sprintId] as const,
  sprintIssues: (wsId: string, sprintId: string) =>
    [...sprintsKeys.all(wsId), "sprint-issues", sprintId] as const,
  recurringTasks: (wsId: string, projectId: string) =>
    [...sprintsKeys.all(wsId), "recurring", projectId] as const,
  issueAssignment: (wsId: string, issueId: string) =>
    [...sprintsKeys.all(wsId), "issue-assignment", issueId] as const,
};

// ---------------------------------------------------------------------------
// Read options
// ---------------------------------------------------------------------------

export function sprintSettingsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintsKeys.settings(wsId, projectId),
    queryFn: () => fetchSprintSettings(projectId),
    enabled: !!wsId && !!projectId,
    staleTime: 30 * 1000,
  });
}

export function projectSprintsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintsKeys.projectSprints(wsId, projectId),
    queryFn: () => fetchSprints(projectId),
    enabled: !!wsId && !!projectId,
    staleTime: 15 * 1000,
  });
}

export function sprintOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintsKeys.sprint(wsId, sprintId),
    queryFn: () => fetchSprint(sprintId),
    enabled: !!wsId && !!sprintId,
    staleTime: 30 * 1000,
  });
}

export function sprintIssuesOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintsKeys.sprintIssues(wsId, sprintId),
    queryFn: () => fetchSprintIssues(sprintId),
    enabled: !!wsId && !!sprintId,
    staleTime: 10 * 1000,
  });
}

export function recurringTasksOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintsKeys.recurringTasks(wsId, projectId),
    queryFn: () => fetchRecurringTasks(projectId),
    enabled: !!wsId && !!projectId,
    staleTime: 30 * 1000,
  });
}

export function issueSprintAssignmentOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: sprintsKeys.issueAssignment(wsId, issueId),
    queryFn: () => fetchIssueSprintAssignment(issueId),
    enabled: !!wsId && !!issueId,
    staleTime: 10 * 1000,
  });
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

export function useUpsertSprintSettings(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: SprintSettingsWriteInput) => upsertSprintSettings(projectId, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.settings(wsId, projectId) });
    },
  });
}

export function useDeleteSprintSettings(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => deleteSprintSettings(projectId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.settings(wsId, projectId) });
      qc.invalidateQueries({ queryKey: sprintsKeys.projectSprints(wsId, projectId) });
    },
  });
}

export function useCreateSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: SprintCreateInput) => createSprint(projectId, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.projectSprints(wsId, projectId) });
    },
  });
}

export function useUpdateSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: SprintUpdateInput }) =>
      updateSprint(id, payload),
    onSuccess: (_data, { id }) => {
      qc.invalidateQueries({ queryKey: sprintsKeys.projectSprints(wsId, projectId) });
      qc.invalidateQueries({ queryKey: sprintsKeys.sprint(wsId, id) });
    },
  });
}

export function useDeleteSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteSprint(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.projectSprints(wsId, projectId) });
    },
  });
}

export function useAssignIssueToSprint(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ issueId, sprintId }: { issueId: string; sprintId: string }) =>
      sprintId === "" ? removeIssueFromSprint(issueId) : assignIssueToSprint(issueId, sprintId),
    onSuccess: (_data, { issueId }) => {
      qc.invalidateQueries({ queryKey: sprintsKeys.issueAssignment(wsId, issueId) });
      qc.invalidateQueries({ queryKey: sprintsKeys.all(wsId) });
      // The project board filters by sprint server-side, so a membership change
      // must refresh the issue lists too (covers create-in-sprint + reassignment).
      qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });
}

export function useCreateRecurringTask(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: RecurringTaskWriteInput) => createRecurringTask(projectId, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.recurringTasks(wsId, projectId) });
    },
  });
}

export function useUpdateRecurringTask(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: RecurringTaskWriteInput }) =>
      updateRecurringTask(id, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.recurringTasks(wsId, projectId) });
    },
  });
}

export function useDeleteRecurringTask(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteRecurringTask(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintsKeys.recurringTasks(wsId, projectId) });
    },
  });
}

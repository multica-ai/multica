import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { sprintKeys } from "./queries";
import type { CreateSprintRequest, UpdateSprintRequest, CompleteSprintRequest } from "../types";

export function useCreateSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateSprintRequest) => api.createSprint(projectId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId, projectId) });
    },
  });
}

export function useUpdateSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sprintId, data }: { sprintId: string; data: UpdateSprintRequest }) =>
      api.updateSprint(sprintId, data),
    onSuccess: (sprint) => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId, projectId) });
      qc.setQueryData(sprintKeys.detail(wsId, sprint.id), sprint);
    },
  });
}

export function useStartSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sprintId: string) => api.startSprint(sprintId),
    onSuccess: (sprint) => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId, projectId) });
      qc.setQueryData(sprintKeys.detail(wsId, sprint.id), sprint);
    },
  });
}

export function useCompleteSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sprintId, data }: { sprintId: string; data: CompleteSprintRequest }) =>
      api.completeSprint(sprintId, data),
    onSuccess: (sprint) => {
      qc.invalidateQueries({ queryKey: sprintKeys.list(wsId, projectId) });
      qc.invalidateQueries({ queryKey: sprintKeys.backlog(wsId, projectId) });
      qc.setQueryData(sprintKeys.detail(wsId, sprint.id), sprint);
    },
  });
}

export function useAddTicketToSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sprintId, ticketId }: { sprintId: string; ticketId: string }) =>
      api.addTicketToSprint(sprintId, ticketId),
    onSuccess: (_data, { sprintId }) => {
      qc.invalidateQueries({ queryKey: sprintKeys.issues(wsId, sprintId) });
      qc.invalidateQueries({ queryKey: sprintKeys.backlog(wsId, projectId) });
    },
  });
}

export function useRemoveTicketFromSprint(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sprintId, ticketId }: { sprintId: string; ticketId: string }) =>
      api.removeTicketFromSprint(sprintId, ticketId),
    onSuccess: (_data, { sprintId }) => {
      qc.invalidateQueries({ queryKey: sprintKeys.issues(wsId, sprintId) });
      qc.invalidateQueries({ queryKey: sprintKeys.backlog(wsId, projectId) });
    },
  });
}

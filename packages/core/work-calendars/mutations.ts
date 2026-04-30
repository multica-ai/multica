import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { workCalendarKeys } from "./queries";
import type {
  CreateWorkCalendarRequest,
  UpdateWorkCalendarRequest,
} from "../types";

export function useCreateWorkCalendar() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateWorkCalendarRequest) =>
      api.createWorkCalendar(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workCalendarKeys.all(wsId) });
    },
  });
}

export function useUpdateWorkCalendar() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: {
      calendarId: string;
      data: UpdateWorkCalendarRequest;
    }) => api.updateWorkCalendar(vars.calendarId, vars.data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: workCalendarKeys.all(wsId) });
      qc.invalidateQueries({
        queryKey: workCalendarKeys.detail(wsId, vars.calendarId),
      });
    },
  });
}

export function useDeleteWorkCalendar() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (calendarId: string) => api.deleteWorkCalendar(calendarId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workCalendarKeys.all(wsId) });
    },
  });
}

export function useImportWorkCalendarFromPDF() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { file: File; name: string }) =>
      api.importWorkCalendarFromPDF(vars.file, vars.name),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workCalendarKeys.all(wsId) });
    },
  });
}

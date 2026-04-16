import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { scheduleKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  Schedule,
  CreateScheduleRequest,
  UpdateScheduleRequest,
  ListSchedulesResponse,
} from "../types";

export function useCreateSchedule() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateScheduleRequest) => api.createSchedule(data),
    onSuccess: (newSchedule) => {
      qc.setQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId), (old) =>
        old && !old.schedules.some((s) => s.id === newSchedule.id)
          ? { ...old, schedules: [...old.schedules, newSchedule], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: scheduleKeys.list(wsId) });
    },
  });
}

export function useUpdateSchedule() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateScheduleRequest) =>
      api.updateSchedule(id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: scheduleKeys.list(wsId) });
      const prevList = qc.getQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId));
      const prevDetail = qc.getQueryData<Schedule>(scheduleKeys.detail(wsId, id));
      qc.setQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId), (old) =>
        old ? { ...old, schedules: old.schedules.map((s) => (s.id === id ? { ...s, ...data } : s)) } : old,
      );
      qc.setQueryData<Schedule>(scheduleKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data } : old,
      );
      return { prevList, prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(scheduleKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(scheduleKeys.detail(wsId, ctx.id), ctx.prevDetail);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: scheduleKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: scheduleKeys.list(wsId) });
    },
  });
}

export function useDeleteSchedule() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteSchedule(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: scheduleKeys.list(wsId) });
      const prevList = qc.getQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId));
      qc.setQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId), (old) =>
        old ? { ...old, schedules: old.schedules.filter((s) => s.id !== id), total: old.total - 1 } : old,
      );
      qc.removeQueries({ queryKey: scheduleKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(scheduleKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: scheduleKeys.list(wsId) });
    },
  });
}

export function useToggleSchedule() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.toggleSchedule(id),
    onSuccess: (updated) => {
      qc.setQueryData<ListSchedulesResponse>(scheduleKeys.list(wsId), (old) =>
        old ? { ...old, schedules: old.schedules.map((s) => (s.id === updated.id ? updated : s)) } : old,
      );
      qc.setQueryData<Schedule>(scheduleKeys.detail(wsId, updated.id), updated);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: scheduleKeys.list(wsId) });
    },
  });
}

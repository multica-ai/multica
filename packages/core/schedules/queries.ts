import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const scheduleKeys = {
  all: (wsId: string) => ["schedules", wsId] as const,
  list: (wsId: string) => [...scheduleKeys.all(wsId), "list"] as const,
  forWorkflow: (wsId: string, workflowId: string) =>
    [...scheduleKeys.all(wsId), "workflow", workflowId] as const,
  detail: (wsId: string, id: string) =>
    [...scheduleKeys.all(wsId), "detail", id] as const,
};

export function scheduleListOptions(wsId: string) {
  return queryOptions({
    queryKey: scheduleKeys.list(wsId),
    queryFn: () => api.listSchedules(),
    select: (data) => data.schedules,
  });
}

export function scheduleDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: scheduleKeys.detail(wsId, id),
    queryFn: () => api.getSchedule(id),
  });
}

export function scheduleListForWorkflowOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: scheduleKeys.forWorkflow(wsId, workflowId),
    queryFn: () => api.listSchedulesForWorkflow(workflowId),
    select: (data) => data.schedules,
  });
}

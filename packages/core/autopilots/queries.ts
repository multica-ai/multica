import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const autopilotKeys = {
  all: (wsId: string) => ["autopilots", wsId] as const,
  list: (wsId: string) => [...autopilotKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "runs", id] as const,
};

export function autopilotListOptions(wsId: string) {
  return queryOptions({
    queryKey: autopilotKeys.list(wsId),
    queryFn: () => api.listAutopilots(),
    select: (data) => data.autopilots,
  });
}

export function autopilotDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.detail(wsId, id),
    queryFn: () => api.getAutopilot(id),
  });
}

export function autopilotRunsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.runs(wsId, id),
    queryFn: () => api.listAutopilotRuns(id),
    select: (data) => data.runs,
  });
}

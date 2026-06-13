import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const epicKeys = {
  all: (wsId: string) => ["epics", wsId] as const,
  list: (wsId: string) => [...epicKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...epicKeys.all(wsId), "detail", id] as const,
};

export function epicListOptions(wsId: string) {
  return queryOptions({
    queryKey: epicKeys.list(wsId),
    queryFn: () => api.listEpics(),
    select: (data) => data.epics,
  });
}

export function epicDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: epicKeys.detail(wsId, id),
    queryFn: () => api.getEpic(id),
  });
}

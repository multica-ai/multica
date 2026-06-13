import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const sprintKeys = {
  all: (wsId: string) => ["sprints", wsId] as const,
  list: (wsId: string) => [...sprintKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...sprintKeys.all(wsId), "detail", id] as const,
};

export function sprintListOptions(wsId: string) {
  return queryOptions({
    queryKey: sprintKeys.list(wsId),
    queryFn: () => api.listSprints(),
    select: (data) => data.sprints,
  });
}

export function sprintDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: sprintKeys.detail(wsId, id),
    queryFn: () => api.getSprint(id),
  });
}

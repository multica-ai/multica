import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  list: (wsId: string) => [...projectKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
  activities: (wsId: string, id: string) =>
    [...projectKeys.detail(wsId, id), "activities"] as const,
};

export function projectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.list(wsId),
    queryFn: () => api.listProjects(),
    select: (data) => data.projects,
  });
}

export function projectActivitiesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.activities(wsId, id),
    queryFn: () => api.listProjectActivities(id),
  });
}

export function projectDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.detail(wsId, id),
    queryFn: () => api.getProject(id),
  });
}

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  list: (wsId: string) => [...projectKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
  members: (wsId: string, id: string) => [...projectKeys.detail(wsId, id), "members"] as const,
  subscription: (wsId: string, id: string) =>
    [...projectKeys.detail(wsId, id), "subscription"] as const,
};

export function projectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.list(wsId),
    queryFn: () => api.listProjects(),
    select: (data) => data.projects,
  });
}

export function projectDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.detail(wsId, id),
    queryFn: () => api.getProject(id),
  });
}

export function projectMembersOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.members(wsId, id),
    queryFn: () => api.getProjectMembers(id),
  });
}

export function projectSubscriptionOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.subscription(wsId, id),
    queryFn: () => api.getProjectSubscription(id),
  });
}

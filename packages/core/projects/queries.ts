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
  // Under `all`, so the project events that already refresh the list refresh
  // the owner's overview too.
  privateOverview: (wsId: string) => [...projectKeys.all(wsId), "private-overview"] as const,
};

// The cached list includes archived projects: it doubles as the id → project
// lookup for every issue row, breadcrumb and filter chip, and an issue in an
// archived project still needs its project's name. Surfaces that offer a
// project as a choice filter archived ones out (see isProjectArchived).
export function projectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.list(wsId),
    queryFn: () => api.listProjects({ include_archived: true }),
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

export function privateProjectOverviewOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.privateOverview(wsId),
    queryFn: () => api.listPrivateProjectsForOwner(),
    select: (data) => data.projects,
  });
}

export function projectSubscriptionOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.subscription(wsId, id),
    queryFn: () => api.getProjectSubscription(id),
  });
}

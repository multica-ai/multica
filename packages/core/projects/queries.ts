import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  // Active list (archived projects hidden) — what the projects page shows
  // by default. Separate cache key from the archived list so toggling the
  // "Show archived" switch doesn't trigger a refetch on the active set.
  list: (wsId: string) => [...projectKeys.all(wsId), "list", "active"] as const,
  // Archived list — populated only when the user toggles "Show archived" on.
  archivedList: (wsId: string) =>
    [...projectKeys.all(wsId), "list", "archived"] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
};

export function projectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.list(wsId),
    // Default: archived projects hidden.
    queryFn: () => api.listProjects(),
    select: (data) => data.projects,
  });
}

/**
 * Like `projectListOptions` but includes archived projects. The two lists
 * are cached separately so the user can flip "Show archived" on and off
 * without re-fetching the active set on each toggle.
 */
export function archivedProjectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.archivedList(wsId),
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

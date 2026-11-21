import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const linkKeys = {
  all: (wsId: string) => ["links", wsId] as const,
  list: (wsId: string, projectId?: string) =>
    [...linkKeys.all(wsId), "list", projectId ?? null] as const,
};

/**
 * Workspace links when `projectId` is omitted, otherwise that project's own
 * links. The two scopes are separate lists; callers that show both compose them.
 */
export function linkListOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: linkKeys.list(wsId, projectId),
    queryFn: () => api.listWorkspaceLinks(projectId, wsId),
  });
}

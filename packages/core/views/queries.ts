import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ViewPage } from "../types";

export const viewKeys = {
  all: (wsId: string) => ["views", wsId] as const,
  list: (wsId: string, page: ViewPage, projectId?: string) =>
    [...viewKeys.all(wsId), page, projectId ?? null] as const,
};

/**
 * Saved views for a page/project scope. Keyed on wsId + page + projectId so
 * switching workspace or page swaps the cache automatically.
 */
export function viewListOptions(wsId: string, page: ViewPage, projectId?: string) {
  return queryOptions({
    queryKey: viewKeys.list(wsId, page, projectId),
    queryFn: () => api.listViews({ page, projectId }),
    select: (data) => data.views,
    enabled: Boolean(wsId),
  });
}

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runKeys = {
  all: (wsId: string) => ["runs", wsId] as const,
  list: (wsId: string, params: { limit: number; offset: number }) =>
    [...runKeys.all(wsId), "list", params.limit, params.offset] as const,
};

export function workspaceTaskRunsOptions(
  wsId: string,
  params: { limit: number; offset: number },
) {
  return queryOptions({
    queryKey: runKeys.list(wsId, params),
    queryFn: () => api.listWorkspaceTaskRuns(wsId, params),
  });
}

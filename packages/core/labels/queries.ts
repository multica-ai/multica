import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export type LabelListScope = {
  projectId?: string | null;
};

export const labelKeys = {
  all: (wsId: string) => ["labels", wsId] as const,
  list: (wsId: string, scope: LabelListScope = {}) =>
    [
      ...labelKeys.all(wsId),
      "list",
      scope.projectId ? "project" : "workspace",
      scope.projectId ?? null,
    ] as const,
  detail: (wsId: string, id: string) =>
    [...labelKeys.all(wsId), "detail", id] as const,
  byIssue: (wsId: string, issueId: string) =>
    [...labelKeys.all(wsId), "issue", issueId] as const,
};

export function labelListOptions(wsId: string, scope: LabelListScope = {}) {
  return queryOptions({
    queryKey: labelKeys.list(wsId, scope),
    queryFn: () => api.listLabels({ project_id: scope.projectId }),
    select: (data) => data.labels,
  });
}

export function issueLabelsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: labelKeys.byIssue(wsId, issueId),
    queryFn: () => api.listLabelsForIssue(issueId),
    select: (data) => data.labels,
    enabled: Boolean(issueId),
  });
}

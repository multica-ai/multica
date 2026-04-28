import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const labelKeys = {
  all: (wsId: string) => ["labels", wsId] as const,
  list: (wsId: string) => [...labelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...labelKeys.all(wsId), "detail", id] as const,
  byIssue: (wsId: string, issueId: string) =>
    [...labelKeys.all(wsId), "issue", issueId] as const,
};

export function labelListOptions(wsId: string) {
  return queryOptions({
    queryKey: labelKeys.list(wsId),
    queryFn: () => api.listLabels(),
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

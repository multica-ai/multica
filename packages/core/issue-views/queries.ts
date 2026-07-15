import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { IssueViewScopeInput } from "../types";

function scopeKey(scope: IssueViewScopeInput) {
  return `${scope.scope_type}:${scope.scope_id ?? "none"}`;
}

export const issueViewKeys = {
  all: (wsId: string) => ["issue-views", wsId] as const,
  lists: (wsId: string) => [...issueViewKeys.all(wsId), "list"] as const,
  list: (wsId: string, scope: IssueViewScopeInput) =>
    [...issueViewKeys.lists(wsId), scopeKey(scope)] as const,
  details: (wsId: string) => [...issueViewKeys.all(wsId), "detail"] as const,
  detail: (wsId: string, id: string) =>
    [...issueViewKeys.details(wsId), id] as const,
};

function retryExceptUnsupported(failureCount: number, error: unknown) {
  return !(error instanceof ApiError && error.status === 404) && failureCount < 2;
}

export function issueViewListOptions(
  wsId: string,
  scope: IssueViewScopeInput,
) {
  return queryOptions({
    queryKey: issueViewKeys.list(wsId, scope),
    queryFn: () => api.listIssueViews(scope),
    retry: retryExceptUnsupported,
  });
}

export function issueViewDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueViewKeys.detail(wsId, id),
    queryFn: () => api.getIssueView(id),
    retry: retryExceptUnsupported,
  });
}

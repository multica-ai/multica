import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** The (scope_type, scope_id) container a surface's views live in. */
export interface IssueViewScope {
  scope_type: "workspace" | "my" | "project";
  scope_id?: string | null;
}

export const issueViewKeys = {
  all: (wsId: string) => ["issue-views", wsId] as const,
  list: (wsId: string, scope: IssueViewScope) =>
    [...issueViewKeys.all(wsId), scope.scope_type, scope.scope_id ?? null] as const,
  detail: (wsId: string, id: string) =>
    [...issueViewKeys.all(wsId), "detail", id] as const,
};

export function issueViewListOptions(wsId: string, scope: IssueViewScope) {
  return queryOptions({
    queryKey: issueViewKeys.list(wsId, scope),
    queryFn: () => api.listIssueViews(scope),
    enabled: !!wsId,
  });
}

/** One view by id — the sidebar pin rows resolve their label/target here. */
export function issueViewDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueViewKeys.detail(wsId, id),
    queryFn: () => api.getIssueView(id),
    enabled: !!wsId && !!id,
  });
}

/**
 * Client-side mirror of the server's canManageIssueView: the owner, or a
 * workspace owner/admin for views shared to the workspace. Drives which
 * affordances render — the server re-checks on every write.
 */
export function canManageIssueView(
  view: { owner_id: string; visibility: string },
  userId: string | null,
  role: string | null | undefined,
): boolean {
  if (!userId) return false;
  if (view.owner_id === userId) return true;
  return view.visibility === "workspace" && (role === "owner" || role === "admin");
}

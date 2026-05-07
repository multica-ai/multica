import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

// Query options for the dashboard. We delegate to existing endpoints in
// Fase 1; Fase 2 (JEH-703) introduces a dedicated /api/cerebro/dashboard
// handler that supports actor filtering server-side.

export const dashboardKeys = {
  all: (wsId: string) => ["cerebro", "dashboard", wsId] as const,
  members: (wsId: string) => [...dashboardKeys.all(wsId), "members"] as const,
  issuesOpen: (wsId: string) =>
    [...dashboardKeys.all(wsId), "issues-open"] as const,
};

export function workspaceMembersOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.members(wsId),
    queryFn: () => api.listMembers(wsId),
    enabled: !!wsId,
    staleTime: 60 * 1000,
  });
}

// Open issues — anything not in done/cancelled. Uses the existing `open_only`
// server filter so we don't paginate through closed issues.
export function workspaceOpenIssuesOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.issuesOpen(wsId),
    queryFn: () => api.listIssues({ open_only: true, limit: 100 }),
    enabled: !!wsId,
    staleTime: 30 * 1000,
  });
}

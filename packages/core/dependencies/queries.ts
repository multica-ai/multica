// CEREBRO-PATCH(issue-dependencies): query keys + options for issue relations.
import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const dependencyKeys = {
  byIssue: (wsId: string, issueId: string) =>
    ["dependencies", wsId, "issue", issueId] as const,
};

export function issueDependenciesOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: dependencyKeys.byIssue(wsId, issueId),
    queryFn: () => api.getIssueDependencies(issueId),
    enabled: Boolean(issueId),
  });
}

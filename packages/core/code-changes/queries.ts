import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const codeChangesKeys = {
  issue: (issueId: string) => ["code-changes", issueId] as const,
};

export const issueCodeChangesOptions = (issueId: string) =>
  queryOptions({
    queryKey: codeChangesKeys.issue(issueId),
    queryFn: () => api.getIssueCodeChanges(issueId),
    enabled: !!issueId,
  });

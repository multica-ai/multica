import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueTypeKeys = {
  all: (wsId: string) => ["issue-types", wsId] as const,
  detail: (wsId: string, id: string) => [...issueTypeKeys.all(wsId), id] as const,
};

export function listIssueTypesOptions(workspaceId: string) {
  return queryOptions({
    queryKey: issueTypeKeys.all(workspaceId),
    queryFn: async () => {
      return await api.listIssueTypes(workspaceId);
    },
    enabled: !!workspaceId,
  });
}

export function getIssueTypeOptions(workspaceId: string, issueTypeId: string) {
  return queryOptions({
    queryKey: issueTypeKeys.detail(workspaceId, issueTypeId),
    queryFn: async () => {
      return await api.getIssueType(workspaceId, issueTypeId);
    },
    enabled: !!workspaceId && !!issueTypeId,
  });
}

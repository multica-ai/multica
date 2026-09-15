import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

export function issueWakeupsOptions(workspaceId: string, issueId: string) {
  return queryOptions({
    queryKey: ["issue-wakeups", workspaceId, issueId],
    queryFn: () => api.listIssueWakeups(issueId),
    enabled: !!workspaceId && !!issueId,
    refetchInterval: 10_000,
  });
}

export function useDisableIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.disableIssueWakeup(issueId, id),
    onSuccess: () => client.invalidateQueries({ queryKey: issueWakeupsOptions(workspaceId, issueId).queryKey }),
  });
}

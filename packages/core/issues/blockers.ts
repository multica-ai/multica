import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

export function useUpdateIssueBlockers(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (change: { blocked_by?: string[]; remove_blocked_by?: string[] }) => api.updateIssue(issueId, change),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/**
 * Edits the edges where this issue is the blocker. Each edge is stored on the
 * issue that waits, so the write goes to that issue and its access rules apply.
 */
export function useUpdateIssueBlocking(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ dependentId, remove = false }: { dependentId: string; remove?: boolean }) =>
      api.updateIssue(dependentId, remove ? { remove_blocked_by: [issueId] } : { blocked_by: [issueId] }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

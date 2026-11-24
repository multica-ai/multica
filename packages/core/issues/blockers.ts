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

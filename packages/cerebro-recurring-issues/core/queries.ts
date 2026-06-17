import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";

import {
  deleteIssueRecurrence,
  fetchIssueRecurrence,
  runIssueRecurrence,
  upsertIssueRecurrence,
} from "./api";
import type { RecurrenceWriteInput } from "./types";

// Query keys scoped by workspace so switching workspaces never leaks cached
// cross-workspace data (CLAUDE.md → workspace-scoped queries).
export const recurringIssuesKeys = {
  all: (wsId: string) => ["cerebro", "recurring-issues", wsId] as const,
  byIssue: (wsId: string, issueId: string) =>
    [...recurringIssuesKeys.all(wsId), "issue", issueId] as const,
};

export function issueRecurrenceOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: recurringIssuesKeys.byIssue(wsId, issueId),
    queryFn: () => fetchIssueRecurrence(issueId),
    enabled: !!wsId && !!issueId,
    staleTime: 15 * 1000,
  });
}

export function useUpsertIssueRecurrence(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: RecurrenceWriteInput) => upsertIssueRecurrence(issueId, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: recurringIssuesKeys.byIssue(wsId, issueId) });
    },
  });
}

export function useDeleteIssueRecurrence(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => deleteIssueRecurrence(issueId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: recurringIssuesKeys.byIssue(wsId, issueId) });
    },
  });
}

export function useRunIssueRecurrence(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (recurrenceId: string) => runIssueRecurrence(recurrenceId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: recurringIssuesKeys.byIssue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });
}

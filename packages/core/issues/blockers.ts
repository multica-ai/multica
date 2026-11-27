import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

/**
 * Settles a blocker write. Only the open issue's detail, where the Relations
 * section reads the edge, is awaited, so the picker closes showing the saved
 * edge. Every other issue query (lists, board, table, the other end of the
 * edge) refetches in the background: awaiting them held the picker open for
 * seconds on a busy workspace (MUL-10169).
 */
function refreshAfterBlockerWrite(qc: QueryClient, wsId: string, issueId: string): Promise<void> {
  const detailKey = issueKeys.detail(wsId, issueId);
  const detail = qc.invalidateQueries({ queryKey: detailKey, exact: true });
  void qc.invalidateQueries({
    queryKey: issueKeys.all(wsId),
    predicate: (query) => !isSameKey(query.queryKey, detailKey),
  });
  return detail;
}

function isSameKey(a: readonly unknown[], b: readonly unknown[]): boolean {
  return a.length === b.length && a.every((part, i) => part === b[i]);
}

export function useUpdateIssueBlockers(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (change: { blocked_by?: string[]; remove_blocked_by?: string[] }) => api.updateIssue(issueId, change),
    onSuccess: () => refreshAfterBlockerWrite(qc, wsId, issueId),
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
    onSuccess: () => refreshAfterBlockerWrite(qc, wsId, issueId),
  });
}

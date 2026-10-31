import { useCallback } from "react";
import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueUnread } from "../types";

/**
 * The viewer's issues with an agent result they have not seen yet. Kept outside
 * `issueKeys` so the broad issue invalidations do not refetch it; it changes
 * only on the viewer's own `issue_unread:changed` events and writes.
 */
export const issueUnreadKeys = {
  all: (wsId: string) => ["issue-unread", wsId] as const,
};

export function issueUnreadOptions(wsId: string) {
  return queryOptions({
    queryKey: issueUnreadKeys.all(wsId),
    queryFn: () => api.listIssueUnread(),
  });
}

/** The viewer's unread mark on one issue, or `null` when it is read. */
export function useIssueUnread(wsId: string, issueId: string): IssueUnread | null {
  const select = useCallback(
    (items: IssueUnread[]) => items.find((item) => item.issue_id === issueId) ?? null,
    [issueId],
  );
  const { data = null } = useQuery({ ...issueUnreadOptions(wsId), select, enabled: !!issueId });
  return data;
}

function useSetIssueUnread(wsId: string, unread: boolean) {
  const qc = useQueryClient();
  const key = issueUnreadKeys.all(wsId);
  return useMutation({
    mutationFn: (issueId: string) => (unread ? api.markIssueUnread(issueId) : api.markIssueRead(issueId)),
    onMutate: async (issueId) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<IssueUnread[]>(key);
      qc.setQueryData<IssueUnread[]>(key, (old) => {
        if (!old) return old;
        const has = old.some((item) => item.issue_id === issueId);
        if (!unread) return has ? old.filter((item) => item.issue_id !== issueId) : old;
        if (has) return old;
        return [{ issue_id: issueId, outcome: "manual", agent_id: null, created_at: new Date().toISOString() }, ...old];
      });
      return { prev };
    },
    onError: (_err, _issueId, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: key });
    },
  });
}

export function useMarkIssueRead(wsId: string) {
  return useSetIssueUnread(wsId, false);
}

export function useMarkIssueUnread(wsId: string) {
  return useSetIssueUnread(wsId, true);
}

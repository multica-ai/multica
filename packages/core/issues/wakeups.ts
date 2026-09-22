import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import type { WorkspaceWakeupFilters } from "../types";
import { api } from "../api";
import { issueKeys } from "./queries";

export function workspaceWakeupSummariesOptions(workspaceId: string) {
  return queryOptions({
    queryKey: ["issue-wakeup-summaries", workspaceId],
    queryFn: () => api.listIssueWakeupSummaries(),
    enabled: !!workspaceId,
    staleTime: 10_000,
  });
}

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
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ["workspace-wakeups", workspaceId],
        }),
        client.invalidateQueries({
          queryKey: issueWakeupsOptions(workspaceId, issueId).queryKey,
        }),
        client.invalidateQueries({
          queryKey: workspaceWakeupSummariesOptions(workspaceId).queryKey,
        }),
        client.invalidateQueries({ queryKey: issueKeys.tasks(issueId) }),
      ]);
    },
  });
}

export function useEnableIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...input
    }: {
      id: string;
      revision: number;
      at?: string;
      rearm?: boolean;
    }) => api.enableIssueWakeup(issueId, id, input),
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ["workspace-wakeups", workspaceId],
        }),
        client.invalidateQueries({
          queryKey: issueWakeupsOptions(workspaceId, issueId).queryKey,
        }),
        client.invalidateQueries({
          queryKey: workspaceWakeupSummariesOptions(workspaceId).queryKey,
        }),
        client.invalidateQueries({ queryKey: issueKeys.tasks(issueId) }),
      ]);
    },
  });
}

export function useEditWakeupInstruction(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: { id: string; instruction: string; expected_instruction: string; revision: number }) =>
      api.editIssueWakeupInstruction(issueId, id, input),
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ["workspace-wakeups", workspaceId] }),
        client.invalidateQueries({ queryKey: issueWakeupsOptions(workspaceId, issueId).queryKey }),
      ]);
    },
  });
}

export function workspaceWakeupsOptions(
  workspaceId: string,
  filters: WorkspaceWakeupFilters,
) {
  return queryOptions({
    queryKey: ["workspace-wakeups", workspaceId, filters],
    queryFn: () => api.listWorkspaceWakeups(filters),
    enabled: !!workspaceId,
    refetchInterval: 10_000,
  });
}

export function useDisableWorkspaceWakeups(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (rows: { id: string; issue_id: string }[]) => {
      const failed: string[] = [];
      // Sequential requests bound load and retain precise partial-failure results.
      for (const row of rows) {
        try {
          await api.disableIssueWakeup(row.issue_id, row.id);
        } catch {
          failed.push(row.id);
        }
      }
      return { failed, succeeded: rows.length - failed.length };
    },
    onSettled: async (_data, _error, rows) => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ["workspace-wakeups", workspaceId],
        }),
        client.invalidateQueries({ queryKey: ["issue-wakeups", workspaceId] }),
        client.invalidateQueries({
          queryKey: ["issue-wakeup-summaries", workspaceId],
        }),
        ...Array.from(new Set(rows.map((r) => r.issue_id))).map((id) =>
          client.invalidateQueries({ queryKey: issueKeys.tasks(id) }),
        ),
      ]);
    },
  });
}

import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import type { IssueWakeupInput, SystemWakeup, WorkspaceSystemWakeup, WorkspaceWakeupFilters } from "../types";
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

export function issueSystemWakeupsOptions(workspaceId: string, issueId: string) {
  return queryOptions({
    queryKey: ["issue-system-wakeups", workspaceId, issueId],
    queryFn: () => api.listIssueSystemWakeups(issueId),
    enabled: !!workspaceId && !!issueId,
    refetchInterval: 10_000,
  });
}

export function useCreateIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: IssueWakeupInput) => api.createIssueWakeup(issueId, input),
    onSettled: () => invalidateIssueWakeups(client, workspaceId, issueId),
  });
}

export function useUpdateIssueSystemWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ rule, ...input }: { rule: SystemWakeup["rule"]; enabled?: boolean; instruction?: string }) =>
      api.updateIssueSystemWakeup(issueId, rule, input),
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: issueSystemWakeupsOptions(workspaceId, issueId).queryKey }),
        client.invalidateQueries({ queryKey: ["workspace-wakeups", workspaceId] }),
      ]),
  });
}

/** Workspace defaults of the platform's rules, for Settings. */
export function workspaceSystemWakeupsOptions(workspaceId: string) {
  return queryOptions({
    queryKey: ["workspace-system-wakeups", workspaceId],
    queryFn: () => api.listWorkspaceSystemWakeups(),
    enabled: !!workspaceId,
  });
}

/** Changing a default reaches every issue that did not set its own. */
export function useUpdateWorkspaceSystemWakeup(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ rule, ...input }: { rule: WorkspaceSystemWakeup["rule"]; enabled?: boolean; instruction?: string }) =>
      api.updateWorkspaceSystemWakeup(rule, input),
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: workspaceSystemWakeupsOptions(workspaceId).queryKey }),
        client.invalidateQueries({ queryKey: ["issue-system-wakeups", workspaceId] }),
        client.invalidateQueries({ queryKey: ["workspace-wakeups", workspaceId] }),
      ]),
  });
}

/** The latest runs a rule started, for its trigger history. */
export function issueWakeupRunsOptions(workspaceId: string, issueId: string, wakeupId: string) {
  return queryOptions({
    queryKey: ["issue-wakeup-runs", workspaceId, issueId, wakeupId],
    queryFn: () => api.listIssueWakeupRuns(issueId, wakeupId),
    enabled: !!workspaceId && !!issueId && !!wakeupId,
    staleTime: 10_000,
  });
}

/** Rules the platform paused on open issues, for board cues. */
export function pausedWakeupsOptions(workspaceId: string) {
  return queryOptions({
    queryKey: ["issue-wakeup-paused", workspaceId],
    queryFn: () => api.listPausedWakeups(),
    enabled: !!workspaceId,
    staleTime: 10_000,
  });
}

function invalidateIssueWakeups(client: ReturnType<typeof useQueryClient>, workspaceId: string, issueId: string) {
  return Promise.all([
    client.invalidateQueries({ queryKey: ["workspace-wakeups", workspaceId] }),
    client.invalidateQueries({ queryKey: issueWakeupsOptions(workspaceId, issueId).queryKey }),
    client.invalidateQueries({ queryKey: workspaceWakeupSummariesOptions(workspaceId).queryKey }),
    client.invalidateQueries({ queryKey: pausedWakeupsOptions(workspaceId).queryKey }),
    client.invalidateQueries({ queryKey: ["issue-wakeup-runs", workspaceId, issueId] }),
    client.invalidateQueries({ queryKey: issueKeys.tasks(issueId) }),
  ]);
}

/** "Wake now": one run of the rule, as if it fired. */
export function useTriggerIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.triggerIssueWakeup(issueId, id),
    onSettled: () => invalidateIssueWakeups(client, workspaceId, issueId),
  });
}

export function useDeleteIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueWakeup(issueId, id),
    onSettled: () => invalidateIssueWakeups(client, workspaceId, issueId),
  });
}

export function useDisableIssueWakeup(workspaceId: string, issueId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.disableIssueWakeup(issueId, id),
    onSettled: () => invalidateIssueWakeups(client, workspaceId, issueId),
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
    onSettled: () => invalidateIssueWakeups(client, workspaceId, issueId),
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
        client.invalidateQueries({ queryKey: ["issue-wakeup-paused", workspaceId] }),
        ...Array.from(new Set(rows.map((r) => r.issue_id))).map((id) =>
          client.invalidateQueries({ queryKey: issueKeys.tasks(id) }),
        ),
      ]);
    },
  });
}

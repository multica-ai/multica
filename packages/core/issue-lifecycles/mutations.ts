import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { projectKeys } from "../projects/queries";
import { issueKeys } from "../issues/queries";
import type { IssueLifecycleResponse } from "../types";
import { issueLifecycleKeys } from "./queries";

function setLifecycleQueryData(
  qc: QueryClient,
  wsId: string,
  lifecycle: IssueLifecycleResponse,
  projectId?: string,
) {
  const queryKey = projectId
    ? issueLifecycleKeys.effective(wsId, projectId)
    : issueLifecycleKeys.all(wsId);
  for (const [key, cached] of qc.getQueriesData<IssueLifecycleResponse>({ queryKey })) {
    if (!cached || (!projectId && cached.lifecycle.id !== lifecycle.lifecycle.id)) continue;
    const options = key[key.length - 1] as { includeArchived?: boolean } | undefined;
    qc.setQueryData<IssueLifecycleResponse>(key, {
      ...lifecycle,
      statuses: options?.includeArchived
        ? lifecycle.statuses
        : lifecycle.statuses.filter((status) => !status.archived_at),
    });
  }
}

export function useUpdateProjectIssueLifecycle() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      projectId,
      mode,
    }: {
      projectId: string;
      mode: "default" | "custom";
    }) => api.updateProjectIssueLifecycle(projectId, mode),
    onSuccess: (lifecycle, { projectId }) => {
      setLifecycleQueryData(qc, wsId, lifecycle, projectId);
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
    onError: (_error, { projectId }) => {
      qc.invalidateQueries({
        queryKey: issueLifecycleKeys.effective(wsId, projectId),
      });
    },
  });
}

export function useUpdateIssueLifecycleStatus() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      lifecycleId,
      statusId,
      data,
    }: {
      lifecycleId: string;
      statusId: string;
      data: Parameters<typeof api.updateIssueLifecycleStatus>[2];
    }) => api.updateIssueLifecycleStatus(lifecycleId, statusId, data),
    onSuccess: (lifecycle) => setLifecycleQueryData(qc, wsId, lifecycle),
    onError: () => qc.invalidateQueries({ queryKey: issueLifecycleKeys.all(wsId) }),
  });
}

export function useArchiveIssueLifecycleStatus() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ lifecycleId, statusId, expectedRevision }: { lifecycleId: string; statusId: string; expectedRevision: number }) =>
      api.archiveIssueLifecycleStatus(lifecycleId, statusId, expectedRevision),
    onSuccess: (lifecycle) => setLifecycleQueryData(qc, wsId, lifecycle),
    onError: () => qc.invalidateQueries({ queryKey: issueLifecycleKeys.all(wsId) }),
  });
}

export function useReorderIssueLifecycleStatuses() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ lifecycleId, statusIds, expectedRevision }: { lifecycleId: string; statusIds: string[]; expectedRevision: number }) =>
      api.reorderIssueLifecycleStatuses(lifecycleId, statusIds, expectedRevision),
    onSuccess: (lifecycle) => setLifecycleQueryData(qc, wsId, lifecycle),
    onError: () => qc.invalidateQueries({ queryKey: issueLifecycleKeys.all(wsId) }),
  });
}

export function useTakeOverAutomationExecution() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, executionId, expectedRevision }: { issueId: string; executionId: string; expectedRevision?: number }) =>
      api.takeOverAutomationExecution(issueId, executionId, expectedRevision),
    onSuccess: ({ issue }, { issueId }) => {
      qc.setQueryData(issueKeys.detail(wsId, issueId), issue);
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueLifecycleKeys.executions(wsId, issueId) });
    },
  });
}

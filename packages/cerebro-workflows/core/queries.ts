import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  activateWorkflow,
  approveHumanCheck,
  fetchActiveWorkflowForIssue,
  fetchLoopState,
  fetchWorkflow,
  fetchWorkflowLoopRuns,
  fetchWorkflowRuns,
  fetchWorkflows,
  regenerateInboundSigningSecret,
  regenerateInboundToken,
  regenerateOutboundSecret,
} from "./api";

export const cerebroWorkflowsKeys = {
  all: (wsId: string) => ["cerebro", "workflows", wsId] as const,
  list: (wsId: string) => [...cerebroWorkflowsKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...cerebroWorkflowsKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, workflowId: string | null, limit: number, offset: number) =>
    [...cerebroWorkflowsKeys.all(wsId), "runs", workflowId ?? "all", limit, offset] as const,
  loopState: (wsId: string, workflowId: string, issueId: string) =>
    [...cerebroWorkflowsKeys.all(wsId), "loop-state", workflowId, issueId] as const,
  activeForIssue: (wsId: string, issueId: string) =>
    [...cerebroWorkflowsKeys.all(wsId), "active-for-issue", issueId] as const,
  loopRuns: (wsId: string, workflowId: string) =>
    [...cerebroWorkflowsKeys.all(wsId), "loop-runs", workflowId] as const,
};

export function cerebroWorkflowsListOptions(wsId: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.list(wsId),
    queryFn: fetchWorkflows,
    enabled: !!wsId,
    staleTime: 15 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function cerebroWorkflowDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.detail(wsId, id),
    queryFn: () => fetchWorkflow(id),
    enabled: !!wsId && !!id,
    staleTime: 30 * 1000,
  });
}

export function cerebroWorkflowRunsOptions(
  wsId: string,
  workflowId: string | null,
  limit = 50,
  offset = 0,
) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.runs(wsId, workflowId, limit, offset),
    queryFn: () => fetchWorkflowRuns({ workflowId, limit, offset }),
    enabled: !!wsId,
    staleTime: 5 * 1000,
    refetchInterval: 10 * 1000,
    placeholderData: (prev) => prev,
  });
}

// Phase 3 (JEH-1108): mutation hooks for the three regenerate endpoints.
// Each invalidates the workflow detail query on success so the masked
// presence-bool flips from false → true in real time after the rotate.
//
// The mutation returns the *response object* (which carries the freshly
// minted plaintext) so the calling component can show it to the user
// exactly once before navigating away. Form components stash it in local
// state; we deliberately do NOT write the plaintext into the query cache
// (the GET response masks it).

export function useRegenerateInboundTokenMutation(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => regenerateInboundToken(workflowId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.detail(wsId, workflowId),
      });
    },
  });
}

export function useRegenerateInboundSigningSecretMutation(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => regenerateInboundSigningSecret(workflowId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.detail(wsId, workflowId),
      });
    },
  });
}

export function useRegenerateOutboundSecretMutation(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => regenerateOutboundSecret(workflowId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.detail(wsId, workflowId),
      });
    },
  });
}

// FIR-2283 — Issue workflow control strip. Polls while a person is watching
// a specific issue run through the recipe; issueId empty disables the query
// (nothing to watch yet).
export function cerebroLoopStateOptions(wsId: string, workflowId: string, issueId: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.loopState(wsId, workflowId, issueId),
    queryFn: () => fetchLoopState(workflowId, issueId),
    enabled: !!wsId && !!workflowId && !!issueId,
    staleTime: 3 * 1000,
    refetchInterval: 5 * 1000,
  });
}

// FIR-2283 v2 point 8 — which recipe (if any) issueId is currently running.
// Used by the "Workflow" picker on the issue itself.
export function cerebroActiveWorkflowForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.activeForIssue(wsId, issueId),
    queryFn: () => fetchActiveWorkflowForIssue(issueId),
    enabled: !!wsId && !!issueId,
  });
}

// FIR-2283 v2 point 7 — the issues that have run through a recipe's loop.
export function cerebroWorkflowLoopRunsOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: cerebroWorkflowsKeys.loopRuns(wsId, workflowId),
    queryFn: () => fetchWorkflowLoopRuns(workflowId),
    enabled: !!wsId && !!workflowId,
  });
}

export function useActivateWorkflowMutation(wsId: string, issueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (workflowId: string) => activateWorkflow(workflowId, issueId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.activeForIssue(wsId, issueId),
      });
    },
  });
}

// FIR-2283 v2 point 8 — activate a chosen recipe on a freshly created issue.
// Unlike useActivateWorkflowMutation (keyed to a known issue), the create-issue
// modal only learns the issue id after the create mutation resolves, so this
// hook takes both ids at call time.
export function useActivateWorkflowForCreate(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { issueId: string; workflowId: string }) =>
      activateWorkflow(input.workflowId, input.issueId),
    onSuccess: (_data, input) => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.activeForIssue(wsId, input.issueId),
      });
    },
  });
}

export function useApproveHumanCheckMutation(wsId: string, workflowId: string, issueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { checkId: string; approved: boolean; note?: string }) =>
      approveHumanCheck(workflowId, input.checkId, {
        issue_id: issueId,
        approved: input.approved,
        note: input.note,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: cerebroWorkflowsKeys.loopState(wsId, workflowId, issueId),
      });
    },
  });
}

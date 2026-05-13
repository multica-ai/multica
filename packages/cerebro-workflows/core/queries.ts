import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  fetchWorkflow,
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

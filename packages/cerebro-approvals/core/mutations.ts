import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { approveApproval, delegateApproval, rejectApproval } from "./api";
import { approvalKeys } from "./queries";
import type { DelegateRequest } from "./types";

export function useApproveApproval() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; note?: string }) =>
      approveApproval(wsId, vars.id, vars.note),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
    },
  });
}

export function useRejectApproval() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; note?: string }) =>
      rejectApproval(wsId, vars.id, vars.note),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
    },
  });
}

export function useDelegateApproval() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; body: DelegateRequest }) =>
      delegateApproval(wsId, vars.id, vars.body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
    },
  });
}

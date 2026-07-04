import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { approvalKeys } from "./queries";

export function useCreateApproval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, issueId, approverType, approverId }: {
      workspaceId: string;
      issueId: string;
      approverType: string;
      approverId: string;
    }) => api.createApproval(workspaceId, issueId, { approver_type: approverType, approver_id: approverId }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: approvalKeys.byIssue(variables.workspaceId, variables.issueId) });
      qc.invalidateQueries({ queryKey: approvalKeys.pending(variables.workspaceId) });
      qc.invalidateQueries({ queryKey: approvalKeys.pendingCount(variables.workspaceId) });
    },
  });
}

export function useApproveApproval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, approvalId, comment }: {
      workspaceId: string;
      approvalId: string;
      comment?: string;
    }) => api.approveApproval(workspaceId, approvalId, { comment }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: approvalKeys.all(variables.workspaceId) });
    },
  });
}

export function useRejectApproval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, approvalId, comment }: {
      workspaceId: string;
      approvalId: string;
      comment?: string;
    }) => api.rejectApproval(workspaceId, approvalId, { comment }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: approvalKeys.all(variables.workspaceId) });
    },
  });
}

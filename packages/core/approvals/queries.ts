import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const approvalKeys = {
  all: (wsId: string) => ["approvals", wsId] as const,
  byIssue: (wsId: string, issueId: string) => [...approvalKeys.all(wsId), "issue", issueId] as const,
  pending: (wsId: string) => [...approvalKeys.all(wsId), "pending"] as const,
  pendingCount: (wsId: string) => [...approvalKeys.all(wsId), "pending-count"] as const,
};

export function listApprovalsByIssueOptions(workspaceId: string, issueId: string) {
  return queryOptions({
    queryKey: approvalKeys.byIssue(workspaceId, issueId),
    queryFn: async () => {
      return await api.listApprovalsByIssue(workspaceId, issueId);
    },
    enabled: !!workspaceId && !!issueId,
  });
}

export function listPendingApprovalsOptions(workspaceId: string) {
  return queryOptions({
    queryKey: approvalKeys.pending(workspaceId),
    queryFn: async () => {
      return await api.listPendingApprovals(workspaceId);
    },
    enabled: !!workspaceId,
  });
}

export function pendingApprovalCountOptions(workspaceId: string) {
  return queryOptions({
    queryKey: approvalKeys.pendingCount(workspaceId),
    queryFn: async () => {
      return await api.getPendingApprovalCount(workspaceId);
    },
    enabled: !!workspaceId,
  });
}

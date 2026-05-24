import { queryOptions } from "@tanstack/react-query";
import { fetchApproval, fetchApprovalAudit, fetchApprovals } from "./api";
import type { ApprovalsFilter } from "./types";

export const approvalKeys = {
  all: (wsId: string) => ["cerebro", "approvals", wsId] as const,
  list: (wsId: string, filter: ApprovalsFilter) =>
    [
      ...approvalKeys.all(wsId),
      "list",
      filter.status,
      filter.limit,
      filter.offset,
    ] as const,
  detail: (wsId: string, id: string) =>
    [...approvalKeys.all(wsId), "detail", id] as const,
  audit: (wsId: string, id: string | null) =>
    [...approvalKeys.all(wsId), "audit", id] as const,
};

export function approvalsListOptions(wsId: string, filter: ApprovalsFilter) {
  return queryOptions({
    queryKey: approvalKeys.list(wsId, filter),
    queryFn: () => fetchApprovals(wsId, filter),
    enabled: !!wsId,
    staleTime: 10 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function approvalDetailOptions(wsId: string, id: string | null) {
  return queryOptions({
    queryKey: approvalKeys.detail(wsId, id ?? ""),
    queryFn: () => fetchApproval(wsId, id ?? ""),
    enabled: !!wsId && !!id,
  });
}

export function approvalAuditOptions(wsId: string, id: string | null) {
  return queryOptions({
    queryKey: approvalKeys.audit(wsId, id),
    queryFn: () => fetchApprovalAudit(wsId, id),
    enabled: !!wsId,
    staleTime: 10 * 1000,
  });
}

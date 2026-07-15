import { queryOptions } from "@tanstack/react-query";
import { fetchAllApprovals, fetchApproval, fetchApprovalAudit, fetchApprovals } from "./api";
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
      filter.origin?.task_id ?? "",
      filter.origin?.issue_id ?? "",
      filter.origin?.chat_session_id ?? "",
      filter.origin?.trigger_comment_id ?? "",
      filter.origin?.surface ?? "",
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

export function approvalsOriginOptions(
  wsId: string,
  filter: Pick<ApprovalsFilter, "status" | "origin">,
) {
  const keyedFilter: ApprovalsFilter = { ...filter, limit: 0, offset: 0 };
  return queryOptions({
    queryKey: approvalKeys.list(wsId, keyedFilter),
    queryFn: () => fetchAllApprovals(wsId, filter),
    enabled: !!wsId,
    staleTime: 10 * 1000,
    placeholderData: (previous) => previous,
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

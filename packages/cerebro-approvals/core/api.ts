import { api, parseWithFallback } from "@multica/core/api";
import {
  approvalAuditListSchema,
  approvalListSchema,
  approvalSchema,
  type Approval,
  type ApprovalAuditList,
  type ApprovalList,
  type ApprovalsFilter,
  type ApproveRequest,
  type DelegateRequest,
} from "./types";

// All calls go through the generic cerebroRequest escape hatch so the upstream
// api client (packages/core/api/client.ts) stays untouched.

const EMPTY_LIST: ApprovalList = {
  approvals: [],
  total: 0,
  pending: 0,
  limit: 50,
  offset: 0,
};

const EMPTY_AUDIT: ApprovalAuditList = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
};

export async function fetchApprovals(
  wsId: string,
  filter: ApprovalsFilter,
): Promise<ApprovalList> {
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  params.set("limit", String(filter.limit));
  params.set("offset", String(filter.offset));
  for (const key of [
    "task_id",
    "issue_id",
    "chat_session_id",
    "trigger_comment_id",
    "surface",
  ] as const) {
    const value = filter.origin?.[key]?.trim();
    if (value) params.set(key, value);
  }
  const path = `/api/workspaces/${wsId}/approvals?${params.toString()}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, approvalListSchema, EMPTY_LIST, { endpoint: path });
}

export async function fetchAllApprovals(
  wsId: string,
  filter: Pick<ApprovalsFilter, "status" | "origin">,
): Promise<ApprovalList> {
  const approvals: Approval[] = [];
  let offset = 0;
  let total = 0;
  let pending = 0;

  do {
    const page = await fetchApprovals(wsId, {
      ...filter,
      limit: 200,
      offset,
    });
    approvals.push(...page.approvals);
    total = page.total;
    pending = page.pending;
    offset += page.approvals.length;
    if (page.approvals.length === 0) break;
  } while (offset < total);

  return { approvals, total, pending, limit: approvals.length, offset: 0 };
}

export async function fetchApproval(
  wsId: string,
  approvalId: string,
): Promise<Approval | null> {
  const path = `/api/workspaces/${wsId}/approvals/${approvalId}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, approvalSchema, null, { endpoint: path });
}

export async function fetchApprovalAudit(
  wsId: string,
  approvalId: string | null,
  limit = 50,
  offset = 0,
): Promise<ApprovalAuditList> {
  const params = new URLSearchParams();
  if (approvalId) params.set("approval_id", approvalId);
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  const path = `/api/workspaces/${wsId}/approvals/audit?${params.toString()}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, approvalAuditListSchema, EMPTY_AUDIT, { endpoint: path });
}

export async function approveApproval(
  wsId: string,
  approvalId: string,
  req: ApproveRequest = {},
): Promise<Approval | null> {
  const path = `/api/workspaces/${wsId}/approvals/${approvalId}/approve`;
  // Only send the optional grant controls when set, so a plain approve keeps the
  // exact prior body shape ({ note }).
  const body: ApproveRequest = { note: req.note ?? "" };
  if (req.grant_for_seconds && req.grant_for_seconds > 0) {
    body.grant_for_seconds = req.grant_for_seconds;
  }
  if (req.grant_at_level) {
    body.grant_at_level = req.grant_at_level;
  }
  const raw = await api.cerebroRequest<unknown>(path, {
    method: "POST",
    body: JSON.stringify(body),
  });
  return parseWithFallback(raw, approvalSchema, null, { endpoint: path });
}

export async function rejectApproval(
  wsId: string,
  approvalId: string,
  note?: string,
): Promise<Approval | null> {
  const path = `/api/workspaces/${wsId}/approvals/${approvalId}/reject`;
  const raw = await api.cerebroRequest<unknown>(path, {
    method: "POST",
    body: JSON.stringify({ note: note ?? "" }),
  });
  return parseWithFallback(raw, approvalSchema, null, { endpoint: path });
}

export async function delegateApproval(
  wsId: string,
  approvalId: string,
  body: DelegateRequest,
): Promise<Approval | null> {
  const path = `/api/workspaces/${wsId}/approvals/${approvalId}/delegate`;
  const raw = await api.cerebroRequest<unknown>(path, {
    method: "POST",
    body: JSON.stringify(body),
  });
  return parseWithFallback(raw, approvalSchema, null, { endpoint: path });
}

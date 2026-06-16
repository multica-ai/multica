import { z } from "zod";

// Approval inbox types (FIR-2131). Backed by the cerebro
// `/api/workspaces/{id}/approvals` surface. Schemas use `.loose()` + defaults
// so backend drift downgrades gracefully (see CLAUDE.md API Response
// Compatibility) rather than white-screening the inbox.

export type ApprovalStatus =
  | "pending"
  | "approved"
  | "rejected"
  | "delegated"
  | "expired"
  | "cancelled";

export type DelegateTargetType = "member" | "group";

export const approvalSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    requester_type: z.string().default("agent"),
    requester_id: z.string().default(""),
    agent_id: z.string().nullable().default(null),
    capability: z.string().default(""),
    resource: z.string().default(""),
    reason: z.string().default(""),
    matched_grant_ids: z.array(z.string()).default([]),
    context: z.unknown().default({}),
    status: z.string().default("pending"),
    decided_by_id: z.string().nullable().default(null),
    decided_at: z.string().nullable().default(null),
    decision_note: z.string().nullable().default(null),
    delegated_to_type: z.string().nullable().default(null),
    delegated_to_id: z.string().nullable().default(null),
    expires_at: z.string().nullable().default(null),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
    // Human-readable enrichment (TECH-3498): the UI shows NAMES, never raw UUIDs.
    // All optional/nullable so an older backend that omits them degrades cleanly.
    requester_name: z.string().nullable().default(null),
    agent_name: z.string().nullable().default(null),
    decided_by_name: z.string().nullable().default(null),
    delegated_to_name: z.string().nullable().default(null),
    issue_id: z.string().nullable().default(null),
    issue_identifier: z.string().nullable().default(null),
    issue_title: z.string().nullable().default(null),
    // The human (member) who triggered the agent run that hit this approval.
    // Optional: an older backend that omits them degrades cleanly.
    triggered_by_id: z.string().nullable().default(null),
    triggered_by_name: z.string().nullable().default(null),
  })
  .loose();

export type Approval = z.infer<typeof approvalSchema>;

export const approvalListSchema = z
  .object({
    approvals: z.array(approvalSchema).default([]),
    total: z.number().default(0),
    pending: z.number().default(0),
    limit: z.number().default(50),
    offset: z.number().default(0),
  })
  .loose();

export type ApprovalList = z.infer<typeof approvalListSchema>;

export const approvalAuditEntrySchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    approval_id: z.string().nullable().default(null),
    action: z.string().default(""),
    actor_type: z.string().nullable().default(null),
    actor_id: z.string().nullable().default(null),
    actor_name: z.string().nullable().default(null),
    via: z.string().default("api"),
    note: z.string().nullable().default(null),
    created_at: z.string().default(""),
  })
  .loose();

export type ApprovalAuditEntry = z.infer<typeof approvalAuditEntrySchema>;

export const approvalAuditListSchema = z
  .object({
    items: z.array(approvalAuditEntrySchema).default([]),
    total: z.number().default(0),
    limit: z.number().default(50),
    offset: z.number().default(0),
  })
  .loose();

export type ApprovalAuditList = z.infer<typeof approvalAuditListSchema>;

export interface ApprovalsFilter {
  status: string | null;
  limit: number;
  offset: number;
}

export interface DelegateRequest {
  to_type: DelegateTargetType;
  to_id: string;
  note?: string;
}

// GrantLevel is the optional permission-chain layer an approver can escalate a
// connection-tool approve to a permanent Allow at (TECH-3498). Empty/omitted =
// no escalation, just the in-flight approve.
export type GrantLevel = "agent" | "runtime" | "workspace";

// ApproveRequest carries the optional "approve for a period" + "grant at a level"
// controls. grant_for_seconds: 0/omitted = a one-shot approve, >0 = reusable for
// that many seconds. grant_at_level escalates to a permanent Allow.
export interface ApproveRequest {
  note?: string;
  grant_for_seconds?: number;
  grant_at_level?: GrantLevel;
}

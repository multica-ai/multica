import { z } from "zod";
import { parseWithFallback } from "../api/schema";

/**
 * Standing approvals (spec 4.4). A policy approves a narrow class of proposals
 * from squads at the right level; the boss sees them afterwards. Only owners and
 * admins create or revoke a policy. The server refuses the classes listed in
 * the spec whatever a policy says.
 */

/** The kinds a boss can write a policy for. Others are refused by the server. */
export const POLICY_KINDS = ["decision", "opportunity"] as const;
export type PolicyKind = (typeof POLICY_KINDS)[number];

export interface PolicyMatch {
  squad_ids?: string[];
  floor_ids?: string[];
  max_cost_micros?: number;
  payload_equals?: Record<string, unknown>;
  tags?: string[];
}

export interface ApprovalPolicy {
  id: string;
  name: string;
  kind: string;
  match: PolicyMatch;
  max_per_week: number;
  /** The last day the policy applies, `YYYY-MM-DD`, or null for no end. */
  valid_until: string | null;
  active: boolean;
  created_by: string;
  created_at: string;
  /** Auto-approvals of the last seven days. */
  week_approvals: number;
  /** A weekly review reversed one of this policy's auto-approvals. */
  flagged: boolean;
}

export interface CreateApprovalPolicyRequest {
  name: string;
  kind: PolicyKind;
  match: PolicyMatch;
  max_per_week?: number;
  valid_until?: string;
}

export interface PolicyAutoApproval {
  proposal_id: string;
  squad_id: string;
  kind: string;
  state: string;
  title: string;
  identifier: string;
  decided_at: string;
  reversed_at: string | null;
  reversal_note: string;
  flagged: boolean;
}

export interface PolicyAutoApprovalHistory {
  auto_approvals: PolicyAutoApproval[];
  /** Any entry in the history was reversed. */
  flagged: boolean;
}

const idList = z
  .array(z.string())
  .nullish()
  .transform((list) => list ?? []);

const PolicyMatchSchema = z
  .object({
    squad_ids: idList,
    floor_ids: idList,
    max_cost_micros: z.number().nullish().transform((v) => v ?? undefined),
    payload_equals: z.record(z.string(), z.unknown()).nullish().transform((v) => v ?? undefined),
    tags: idList,
  })
  .loose();

export const ApprovalPolicySchema = z
  .object({
    id: z.string().default(""),
    name: z.string().default(""),
    kind: z.string().default(""),
    match: PolicyMatchSchema.nullish().transform((m) => (m ?? { squad_ids: [], floor_ids: [], tags: [] }) as PolicyMatch),
    max_per_week: z.number().int().default(3),
    valid_until: z.string().nullish().transform((d) => d ?? null),
    active: z.boolean().default(true),
    created_by: z.string().default(""),
    created_at: z.string().default(""),
    week_approvals: z.number().int().default(0),
    flagged: z.boolean().default(false),
  })
  .loose()
  .transform((p): ApprovalPolicy => p);

export const ApprovalPolicyListSchema = z
  .object({
    policies: z.array(z.unknown()).nullish().transform((list) => list ?? []),
  })
  .loose();

export const PolicyAutoApprovalSchema = z
  .object({
    proposal_id: z.string().default(""),
    squad_id: z.string().default(""),
    kind: z.string().default(""),
    state: z.string().default(""),
    title: z.string().default(""),
    identifier: z.string().default(""),
    decided_at: z.string().default(""),
    reversed_at: z.string().nullish().transform((at) => at ?? null),
    reversal_note: z.string().default(""),
    flagged: z.boolean().default(false),
  })
  .loose()
  .transform((a): PolicyAutoApproval => a);

export const PolicyAutoApprovalHistorySchema = z
  .object({
    auto_approvals: z.array(z.unknown()).nullish().transform((list) => list ?? []),
    flagged: z.boolean().default(false),
  })
  .loose();

export const EMPTY_APPROVAL_POLICIES: ApprovalPolicy[] = [];

/** Reads the Policies list. A malformed entry is dropped, not the whole list. */
export function parseApprovalPolicies(raw: unknown, endpoint: string): ApprovalPolicy[] {
  const list = parseWithFallback<{ policies: unknown[] }>(raw, ApprovalPolicyListSchema, { policies: [] }, { endpoint });
  const out: ApprovalPolicy[] = [];
  for (const entry of list.policies) {
    const parsed = ApprovalPolicySchema.safeParse(entry);
    if (parsed.success) out.push(parsed.data);
  }
  return out;
}

/** Reads one policy; a malformed one falls back to an inactive empty policy. */
export function parseApprovalPolicy(raw: unknown, endpoint: string): ApprovalPolicy {
  return parseWithFallback<ApprovalPolicy>(raw, ApprovalPolicySchema, emptyPolicy(), { endpoint });
}

function emptyPolicy(): ApprovalPolicy {
  return {
    id: "",
    name: "",
    kind: "",
    match: {},
    max_per_week: 3,
    valid_until: null,
    active: false,
    created_by: "",
    created_at: "",
    week_approvals: 0,
    flagged: false,
  };
}

/** Reads a policy's history; a malformed one reads as empty. */
export function parsePolicyAutoApprovals(raw: unknown, endpoint: string): PolicyAutoApprovalHistory {
  const history = parseWithFallback<{ auto_approvals: unknown[]; flagged: boolean }>(
    raw,
    PolicyAutoApprovalHistorySchema,
    { auto_approvals: [], flagged: false },
    { endpoint },
  );
  const entries: PolicyAutoApproval[] = [];
  for (const entry of history.auto_approvals) {
    const parsed = PolicyAutoApprovalSchema.safeParse(entry);
    if (parsed.success) entries.push(parsed.data);
  }
  return { auto_approvals: entries, flagged: history.flagged };
}

// FIR-3212 Approval slice — client for the approval-consequences preview.
//
// GET /api/agents/{id}/capabilities/approval-impact?fields=a,b answers "if I
// approve this proposal, what actually changes about what this agent does?". The
// classification is computed on the server, next to the support matrix it is
// tested against; this client only carries it. Nothing here re-derives a
// consequence from a handling string — that would put the vocabulary in two
// places, and the copy in the browser is the one that never gets checked against
// an installed CLI.
//
// Per the API Response Compatibility rules (CLAUDE.md) every field is parsed with
// an explicit fallback rather than cast. The failure mode this guards is
// specific: the panel renders inside the change-request queue, so a throw here
// would take the field diff — and the Approve button — down with it. A drifting
// backend must degrade this panel to "we cannot say" and leave the approval flow
// working.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import {
  AgentCapabilityRuntimeOptionsSchema,
  EMPTY_RUNTIME_OPTIONS,
} from "./api";

const ApprovalFieldConsequenceSchema = z
  .object({
    field: z.string().catch(""),
    // delivered_by is a server enum (engine|multica|none); an unknown value must
    // survive to the UI's default branch rather than throw.
    delivered_by: z.string().catch(""),
    exec_field: z.string().catch(""),
    handling: z.string().catch(""),
    // takes_effect | no_effect_logged | no_effect_silent | no_runtime_effect |
    // unknown_field
    consequence: z.string().catch(""),
    // The bit the warning order is built on: the engine drops this value with no
    // log line, so the approver would keep believing the change landed.
    silent: z.boolean().catch(false),
  })
  .loose();

const ApprovalPromptEffectSchema = z
  .object({
    native: z.boolean().catch(false),
    modes: z.array(z.string()).catch([]),
    // native | prepended | ignored
    delivery: z.string().catch(""),
  })
  .loose();

const EMPTY_IMPACT = {
  status: "unknown",
  provider: "",
  fields: [],
  effective: [],
  ineffective: [],
  silently_ineffective: [],
};

const ApprovalImpactSchema = z
  .object({
    // status=unknown means the matrix has no entry for this agent's engine. It
    // never means the proposal does nothing — the UI must say "we cannot say"
    // and enumerate nothing.
    status: z.string().catch("unknown"),
    provider: z.string().catch(""),
    fields: z.array(ApprovalFieldConsequenceSchema).catch([]),
    effective: z.array(z.string()).catch([]),
    ineffective: z.array(z.string()).catch([]),
    silently_ineffective: z.array(z.string()).catch([]),
    system_prompt: ApprovalPromptEffectSchema.optional(),
  })
  .loose()
  .catch(EMPTY_IMPACT);

export const AgentCapabilityApprovalSchema = z
  .object({
    agent_id: z.string().catch(""),
    runtime: AgentCapabilityRuntimeOptionsSchema.catch(EMPTY_RUNTIME_OPTIONS),
    impact: ApprovalImpactSchema.default(EMPTY_IMPACT),
  })
  .loose();

export type AgentCapabilityApprovalFieldConsequence = z.infer<
  typeof ApprovalFieldConsequenceSchema
>;
export type AgentCapabilityApprovalPromptEffect = z.infer<
  typeof ApprovalPromptEffectSchema
>;
export type AgentCapabilityApprovalImpact = z.infer<
  typeof ApprovalImpactSchema
>;
export type AgentCapabilityApproval = z.infer<
  typeof AgentCapabilityApprovalSchema
>;

const EMPTY_APPROVAL: AgentCapabilityApproval = {
  agent_id: "",
  runtime: EMPTY_RUNTIME_OPTIONS,
  impact: EMPTY_IMPACT,
};

export async function getAgentCapabilityApproval(
  agentId: string,
  changedFields: string[],
): Promise<AgentCapabilityApproval> {
  const fields = encodeURIComponent(changedFields.join(","));
  const path = `/api/agents/${agentId}/capabilities/approval-impact?fields=${fields}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, AgentCapabilityApprovalSchema, EMPTY_APPROVAL, {
    endpoint: path,
  }) as AgentCapabilityApproval;
}

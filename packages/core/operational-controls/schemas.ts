import { z } from "zod";

const UUIDSchema = z.string().uuid();
const TimestampSchema = z.string().datetime({ offset: true });
const DigestSchema = z.string().regex(/^sha256:[0-9a-f]{64}$/);
const sensitiveMarkerPattern =
  /canary|bearer |password=|token=|api_key=|-----begin|sk-|ghp_/i;
const ExactIdentitySchema = z
  .string()
  .regex(/^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$/)
  .refine((value) => !sensitiveMarkerPattern.test(value));
const SafeDiagnosticSchema = z
  .string()
  .min(1)
  .max(256)
  .refine((value) => !sensitiveMarkerPattern.test(value));
const ByteCountSchema = z.number().int().min(0).max(134_217_728);
const DurationSchema = z.number().int().min(0).max(86_400_000);

export const ToolTransportKindSchema = z.enum([
  "managed_mcp",
  "managed_native",
]);

export const AgentToolPolicyRuleSchema = z
  .object({
    transport_kind: ToolTransportKindSchema,
    server_key: ExactIdentitySchema,
    tool_name: ExactIdentitySchema,
    schema_digest: DigestSchema,
    effect: z.enum(["allow", "require_approval"]),
  })
  .strict();

export const AgentToolPolicySchema = z
  .object({
    configured: z.boolean(),
    revision: z.number().int().min(0).optional(),
    status: z.string().min(1).max(64).optional(),
    policy_digest: DigestSchema.optional(),
    default_effect: z.literal("deny").optional(),
    rules: z.array(AgentToolPolicyRuleSchema),
  })
  .strict();

export const ReplaceAgentToolPolicyRequestSchema = z
  .object({
    expected_revision: z.number().int().min(0),
    rules: z.array(AgentToolPolicyRuleSchema).max(1_000),
  })
  .strict();

export const AgentToolActionEventTypeSchema = z.enum([
  "requested",
  "policy_allowed",
  "policy_denied",
  "approval_requested",
  "approval_approved",
  "approval_denied",
  "approval_expired",
  "approval_consumed",
  "started",
  "succeeded",
  "failed",
  "cancelled",
]);

export const AgentToolActionEventSchema = z
  .object({
    id: UUIDSchema,
    workspace_id: UUIDSchema,
    agent_id: UUIDSchema,
    task_id: UUIDSchema,
    issue_id: UUIDSchema.optional(),
    invocation_id: UUIDSchema,
    approval_request_id: UUIDSchema.optional(),
    transport_kind: ToolTransportKindSchema,
    server_key: ExactIdentitySchema,
    tool_name: ExactIdentitySchema,
    schema_digest: DigestSchema,
    coverage_kind: z.enum([
      "managed_mcp",
      "managed_native",
      "declaration_only",
    ]),
    event_type: AgentToolActionEventTypeSchema,
    argument_bytes: ByteCountSchema.optional(),
    result_bytes: ByteCountSchema.optional(),
    duration_ms: DurationSchema.optional(),
    outcome_code: z
      .enum([
        "allowed",
        "denied",
        "approval_required",
        "approved",
        "consumed",
        "expired",
        "cancelled",
        "started",
        "succeeded",
        "failed",
      ])
      .optional(),
    error_class: z
      .enum([
        "transport",
        "timeout",
        "cancelled",
        "invalid_request",
        "provider",
        "internal",
        "audit",
        "schema_drift",
        "unsupported",
        "policy",
      ])
      .optional(),
    actor_user_id: UUIDSchema.optional(),
    created_at: TimestampSchema,
  })
  .strict();

export const AgentToolActionListResponseSchema = z
  .object({
    items: z.array(AgentToolActionEventSchema),
    next_cursor: z.string().min(1).max(2_048).optional(),
  })
  .strict();

export const AgentToolApprovalStatusSchema = z.enum([
  "pending",
  "approved",
  "consumed",
  "denied",
  "expired",
  "cancelled",
]);

export const AgentToolApprovalSchema = z
  .object({
    id: UUIDSchema,
    workspace_id: UUIDSchema,
    agent_id: UUIDSchema,
    task_id: UUIDSchema,
    issue_id: UUIDSchema.optional(),
    invocation_id: UUIDSchema,
    transport_kind: ToolTransportKindSchema,
    server_key: ExactIdentitySchema,
    tool_name: ExactIdentitySchema,
    schema_digest: DigestSchema,
    policy_revision: z.number().int().min(0),
    schema_field_names: z.array(ExactIdentitySchema).max(1_000),
    argument_bytes: ByteCountSchema,
    status: AgentToolApprovalStatusSchema,
    requested_at: TimestampSchema,
    expires_at: TimestampSchema,
    decided_at: TimestampSchema.optional(),
    consumed_at: TimestampSchema.optional(),
    cancelled_at: TimestampSchema.optional(),
    decider_user_id: UUIDSchema.optional(),
  })
  .strict();

export const AgentToolApprovalListResponseSchema = z
  .object({
    items: z.array(AgentToolApprovalSchema),
    next_cursor: z.string().min(1).max(2_048).optional(),
  })
  .strict();

export const AgentToolApprovalDecisionRequestSchema = z
  .object({
    decision: z.enum(["approve", "deny"]),
    reason_code: ExactIdentitySchema,
    expected_status: z.literal("pending"),
  })
  .strict();

export const OperationalCapabilitySchema = z
  .object({
    name: ExactIdentitySchema,
    transport_kind: ToolTransportKindSchema,
    provider_family: ExactIdentitySchema,
    supported: z.boolean(),
    offline_reason: SafeDiagnosticSchema.optional(),
  })
  .strict();

export const OperationalCapabilityListResponseSchema = z
  .object({
    capabilities: z.array(OperationalCapabilitySchema),
  })
  .strict();

export const OperationalSummarySchema = z
  .object({
    workspace_id: UUIDSchema,
    days: z.number().int().min(1).max(365),
    timezone: z.string().min(1).max(128),
    project_id: UUIDSchema.optional(),
    generated_at: TimestampSchema,
    pending: z.number().int().min(0),
    approved: z.number().int().min(0),
    denied: z.number().int().min(0),
    expired: z.number().int().min(0),
    failed: z.number().int().min(0),
    intercepted_invocation_count: z.number().int().min(0),
    declaration_only_count: z.number().int().min(0),
    median_decision_time_ms: DurationSchema.nullable(),
    configured_agent_capability_gaps: z.number().int().min(0),
  })
  .strict();

export const OperationalControlsChangedPayloadSchema = z
  .object({ workspace_id: UUIDSchema })
  .strict();

export type AgentToolPolicy = z.infer<typeof AgentToolPolicySchema>;
export type AgentToolPolicyRule = z.infer<typeof AgentToolPolicyRuleSchema>;
export type ReplaceAgentToolPolicyRequest = z.infer<
  typeof ReplaceAgentToolPolicyRequestSchema
>;
export type AgentToolActionEvent = z.infer<typeof AgentToolActionEventSchema>;
export type AgentToolActionEventType = z.infer<
  typeof AgentToolActionEventTypeSchema
>;
export type AgentToolActionListResponse = z.infer<
  typeof AgentToolActionListResponseSchema
>;
export type AgentToolApproval = z.infer<typeof AgentToolApprovalSchema>;
export type AgentToolApprovalStatus = z.infer<
  typeof AgentToolApprovalStatusSchema
>;
export type AgentToolApprovalListResponse = z.infer<
  typeof AgentToolApprovalListResponseSchema
>;
export type AgentToolApprovalDecisionRequest = z.infer<
  typeof AgentToolApprovalDecisionRequestSchema
>;
export type OperationalCapabilityListResponse = z.infer<
  typeof OperationalCapabilityListResponseSchema
>;
export type OperationalSummary = z.infer<typeof OperationalSummarySchema>;
export type OperationalControlsChangedPayload = z.infer<
  typeof OperationalControlsChangedPayloadSchema
>;

export interface AgentToolActionListParams {
  event_type?: AgentToolActionEventType;
  since?: string;
  cursor?: string;
  limit?: number;
}

export interface AgentToolApprovalListParams {
  agent_id?: string;
  status?: AgentToolApprovalStatus;
  cursor?: string;
  limit?: number;
}

export interface OperationalSummaryParams {
  days: number;
  tz: string;
  project_id?: string;
}

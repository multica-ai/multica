import { z } from "zod";
import type {
  ActivateWorkflowResponse,
  ActiveWorkflowForIssueResponse,
  CerebroWorkflow,
  LoopStateResponse,
  RegenerateInboundSigningSecretResponse,
  RegenerateInboundTokenResponse,
  RegenerateOutboundSecretResponse,
  WorkflowRunsListResponse,
  WorkflowsListResponse,
} from "./types";

// ---------------------------------------------------------------------------
// Zod schemas for the cerebro-workflows REST surface — the boundary defense
// against API drift that CLAUDE.md "API Response Compatibility" mandates for
// any installed-app consuming a server it may outlive.
//
// Schemas are intentionally LENIENT:
//   - Enums stored as `z.string()` so a future server-side enum value
//     downgrades to a generic UI fallback instead of failing parse.
//   - Every object schema ends with `.passthrough()` so unknown server-side
//     fields pass through unchanged. (We use `.passthrough()` not zod 4's
//     `.loose()` because the catalog still resolves to zod 3.x in practice;
//     the two methods are equivalent in semantics.) zod's default `.object()`
//     is STRIP, which silently deletes new fields and creates "field neither
//     showed up in the UI" mysteries.
//   - Optional / mask-on-read fields are defaulted to safe sentinels
//     (`null` for the token, `false` for the *_set bools) so an older backend
//     that omits them entirely doesn't white-screen the editor.
//
// The leniency is what lets `parseWithFallback` return cast-to-T values
// without claiming the server actually constrains every literal type the TS
// side narrows on.
// ---------------------------------------------------------------------------

// Phase 3 (JEH-1108): inbound webhook token lives on the workflow row; the
// two *_set bools are mask-on-read presence flags.
export const workflowSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    project_id: z.string().optional(),
    name: z.string(),
    enabled: z.boolean(),
    trigger_type: z.string(),
    trigger_config: z.unknown(),
    conditions: z.unknown(),
    action_type: z.string(),
    action_config: z.unknown(),
    editor_mode: z.string().default("form"),
    editor_layout: z.unknown().optional(),
    created_by_id: z.string(),
    created_by_type: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
    // Phase 3 — default `null` / `false` so an older backend (no phase-3
    // columns) doesn't break the UI. The form's WebhookInboundFields handles
    // both undefined and null tokens identically (renders "save first").
    inbound_webhook_token: z.string().nullable().optional(),
    inbound_signing_secret_set: z.boolean().optional().default(false),
    outbound_webhook_secret_set: z.boolean().optional().default(false),
    // FIR-2283. Default "standard" so an older backend that omits the field
    // entirely renders as the pre-existing workflow type, never crashes.
    workflow_type: z.string().optional().default("standard"),
    loop_spec: z.unknown().optional(),
  })
  .passthrough();

export const workflowsListSchema = z
  .object({
    workflows: z.array(workflowSchema).default([]),
  })
  .passthrough();

export const workflowRunSchema = z
  .object({
    id: z.string(),
    workflow_id: z.string(),
    workspace_id: z.string(),
    target_issue_id: z.string().optional(),
    task_id: z.string().optional(),
    status: z.string(),
    attempt: z.number().default(0),
    error: z.string().optional(),
    started_at: z.string().optional(),
    finished_at: z.string().optional(),
    next_retry_at: z.string().optional(),
    created_at: z.string(),
  })
  .passthrough();

export const workflowRunsListSchema = z
  .object({
    runs: z.array(workflowRunSchema).default([]),
    limit: z.number().default(50),
    offset: z.number().default(0),
  })
  .passthrough();

// Regenerate endpoint responses — the plaintext token / secret crosses the
// network exactly once. We default to empty strings on failure so the UI's
// "show this to the user before navigating away" path renders a benign empty
// block instead of crashing on `.length`.
export const regenerateInboundTokenSchema = z
  .object({
    inbound_webhook_token: z.string().default(""),
    inbound_webhook_url: z.string().default(""),
  })
  .passthrough();

export const regenerateInboundSigningSecretSchema = z
  .object({
    inbound_signing_secret: z.string().default(""),
  })
  .passthrough();

export const regenerateOutboundSecretSchema = z
  .object({
    outbound_webhook_secret: z.string().default(""),
  })
  .passthrough();

// FIR-2283 — control strip live state.
export const pendingHumanCheckSchema = z
  .object({
    check_id: z.string(),
    prompt: z.string().default(""),
    assignee_type: z.string(),
    assignee_id: z.string(),
  })
  .passthrough();

export const loopStateSchema = z
  .object({
    round: z.number().default(0),
    max_iterations: z.number().optional(),
    stopped: z.boolean().default(false),
    stop_reason: z.string().optional(),
    pending_human_checks: z.array(pendingHumanCheckSchema).default([]),
  })
  .passthrough();

// FIR-2283 v2 point 8 — "per-issue workflow activation".
export const activateWorkflowSchema = z
  .object({
    activated: z.boolean().default(false),
    workflow_id: z.string().default(""),
    issue_id: z.string().default(""),
  })
  .passthrough();

export const activeWorkflowForIssueSchema = z
  .object({
    active: z.boolean().default(false),
    workflow_id: z.string().optional(),
  })
  .passthrough();

// Empty / fallback values used by parseWithFallback at call sites. Defined
// once here so the values stay consistent across api.ts and queries.ts.
export const EMPTY_WORKFLOWS_LIST: WorkflowsListResponse = { workflows: [] };
export const EMPTY_WORKFLOW_RUNS_LIST: WorkflowRunsListResponse = {
  runs: [],
  limit: 0,
  offset: 0,
};
export const EMPTY_REGENERATE_INBOUND_TOKEN: RegenerateInboundTokenResponse = {
  inbound_webhook_token: "",
  inbound_webhook_url: "",
};
export const EMPTY_REGENERATE_INBOUND_SECRET: RegenerateInboundSigningSecretResponse =
  {
    inbound_signing_secret: "",
  };
export const EMPTY_REGENERATE_OUTBOUND_SECRET: RegenerateOutboundSecretResponse =
  {
    outbound_webhook_secret: "",
  };

// EMPTY_WORKFLOW is the lenient fallback for fetchWorkflow — the form's
// detail-query starts from this when the server returns a malformed shape,
// keeping the editor renderable instead of white-screening. Every field that
// the form reads must be present (with a safe default) so the field-render
// path can `=== true` / `?.` against it without crashing.
export const EMPTY_WORKFLOW: CerebroWorkflow = {
  id: "",
  workspace_id: "",
  name: "",
  enabled: false,
  trigger_type: "status_changed",
  trigger_config: {},
  conditions: [],
  action_type: "set_status",
  action_config: {},
  editor_mode: "form",
  created_by_id: "",
  created_by_type: "member",
  created_at: "",
  updated_at: "",
  inbound_webhook_token: undefined,
  inbound_signing_secret_set: false,
  outbound_webhook_secret_set: false,
  workflow_type: "standard",
};

export const EMPTY_LOOP_STATE: LoopStateResponse = {
  round: 0,
  stopped: false,
  pending_human_checks: [],
};

export const EMPTY_ACTIVATE_WORKFLOW: ActivateWorkflowResponse = {
  activated: false,
  workflow_id: "",
  issue_id: "",
};

export const EMPTY_ACTIVE_WORKFLOW_FOR_ISSUE: ActiveWorkflowForIssueResponse = {
  active: false,
};

export type CerebroWorkflowTriggerType =
  | "status_changed"
  | "due_date_reached"
  | "due_time_reached"
  // Phase 3 (JEH-1108).
  | "cron"
  | "webhook_inbound"
  | "comment_mention"
  | "all_children_done"
  | "sub_issue_created";

export type CerebroWorkflowActionType =
  | "set_status"
  | "create_sub_issue"
  | "send_reminder"
  | "run_skill"
  | "comment_on_issue"
  | "route_by_domain"
  | "escalate_to_owner"
  // Phase 3 (JEH-1108).
  | "webhook_outbound"
  | "reassign_issue";

export type CerebroWorkflowRunStatus =
  | "queued"
  | "running"
  | "success"
  | "failed"
  | "escalated";

export type CerebroWorkflowEditorMode = "form" | "canvas";

// FIR-2283 — Issue workflow. "standard" is the existing trigger -> conditions
// -> action rule (unchanged). "issue_loop" is a Plan/Build/Delivery-gate/Done
// recipe: it carries a LoopSpec instead of a trigger/action, and the server
// compiles it onto the loop engine (see loop_spec below).
export type CerebroWorkflowType = "standard" | "issue_loop";

// Phase-3 trigger config shapes (mirrors server/internal/cerebro/workflows/types.go).
export interface TriggerConfigCron {
  schedule_expr: string;
  timezone?: string;
}

export type CerebroCommentMatchMode = "agent" | "member" | "keyword" | "regex";

export interface TriggerConfigCommentMention {
  match_mode: CerebroCommentMatchMode;
  target: string;
}

export interface TriggerConfigSubIssueCreated {
  parent_issue_id?: string;
}

// Phase-3 action config shapes.
export interface ActionConfigWebhookOutbound {
  url: string;
  headers?: Record<string, string>;
  include_issue_snapshot?: boolean;
}

export type CerebroAssigneeType = "member" | "agent";

export interface ActionConfigReassignIssue {
  assignee_id: string;
  assignee_type: CerebroAssigneeType;
}

export interface CerebroWorkflow {
  id: string;
  workspace_id: string;
  project_id?: string;
  name: string;
  enabled: boolean;
  trigger_type: CerebroWorkflowTriggerType;
  trigger_config: unknown;
  conditions: unknown;
  action_type: CerebroWorkflowActionType;
  action_config: unknown;
  // Phase 2 (JEH-1103). New rows default to "form" on the server when
  // omitted, so older clients that don't send these fields keep getting
  // the form editor. editor_layout is null for form-mode rows.
  editor_mode: CerebroWorkflowEditorMode;
  editor_layout?: unknown;
  created_by_id: string;
  created_by_type: "member" | "agent";
  created_at: string;
  updated_at: string;
  // Phase 3 (JEH-1108). The inbound token is part of the integration URL
  // and is intentionally visible after creation. The two secrets are
  // mask-on-read — only the presence-bool is returned. Server omits the
  // token field entirely (omitempty) until RegenerateInboundToken runs,
  // so default to undefined here too.
  inbound_webhook_token?: string;
  inbound_signing_secret_set?: boolean;
  outbound_webhook_secret_set?: boolean;
  // FIR-2283. Absent on older cached responses -> treat as "standard".
  workflow_type?: CerebroWorkflowType;
  loop_spec?: LoopSpec;
}

// --- FIR-2283 Issue workflow: loop_spec wire shape ---
// Mirrors server/internal/cerebro/loops.Spec + the issue-specific bindings
// loops.CompileParams needs (see materialize_issue_loop.go's
// issueLoopSpecWire) — the recipe designed on the Issue workflow surface.

export type LoopCheckType = "programmatic" | "judge" | "human";
export type LoopAssigneeType = "agent" | "member";

// "Confirmed by" in the UI. Maps 1:1 to LoopCheckType; kept as a distinct
// display-facing alias so the editor's copy can read "A command" / "An AI
// review" / "A person" without every callsite re-deriving the label.
export const CONFIRMED_BY_OPTIONS: ReadonlyArray<{
  value: LoopCheckType;
  label: string;
}> = [
  { value: "programmatic", label: "A command" },
  { value: "judge", label: "An AI review" },
  { value: "human", label: "A person" },
];

export interface LoopVerification {
  id: string;
  type: LoopCheckType;
  /** Short human-readable name, e.g. "Test suite". Display-only. */
  label?: string;

  // Programmatic ("A command").
  check?: string[]; // argv array, never a shell string
  expect?: "exit_zero";

  // Judge ("An AI review"). Runs a skill (preferred) or a free-text prompt
  // ("rubric") — "Runs: A skill / A prompt" in the editor.
  rubric?: string;
  skill?: string;

  // Human ("A person").
  prompt?: string;

  // Assignee for judge/human checks — "Assign to" in the editor, showing
  // both agents and people in one list.
  assignee_type?: LoopAssigneeType;
  assignee_id?: string;
  model?: string;
  thinking_level?: string;
}

export interface LoopCaps {
  max_iterations: number;
  max_revisions: number;
  no_progress_stalls: number;
}

// One ordered build phase in a multi-phase loop (FIR-2283 followup point 6).
// Each phase runs its own build skill and is gated by its own delivery review
// (verification) that must pass before the next phase's build starts.
export interface LoopBuildPhase {
  /** Display name, e.g. "Backend". Names the phase's build/review sessions. */
  name?: string;
  build_skill: string;
  build_agent_id?: string;
  model?: string;
  thinking_level?: string;
  goal?: string;
  verification: LoopVerification[];
}

export interface LoopSpec {
  version: 1;
  // FIR-2283 v2 point 4 — one free-text prompt (skill-taggable) replaces the
  // old fixed Goal / Definition-of-done pair. Both stay optional on the wire
  // (the backend spec already treats them as human-notes, not required); the
  // editor now writes only `goal` and leaves `definition_of_done` unset.
  goal?: string;
  definition_of_done?: string;
  verification: LoopVerification[];
  caps: LoopCaps;
  planning?: boolean;
  // Gates on the Plan step (FIR-2283 v2 point 6) — only meaningful when
  // planning is true. Unlike verification, a plan gate does NOT require a
  // programmatic entry: there is no code yet to run a command against
  // during planning, so a judge-only criterion (e.g. an adversarial AI
  // review of the plan) is valid on its own.
  plan_gate?: LoopVerification[];

  // Build bindings. Used for a single-phase loop. When `phases` is set (a
  // multi-phase loop), these are ignored — each phase carries its own build
  // skill/agent and its own delivery gate.
  build_agent_id: string;
  build_skill: string;
  build_model?: string;
  build_thinking?: string;

  // Multi-phase build chain (FIR-2283 followup point 6). When non-empty, the
  // build is split into ordered phases, each gated by its own review that must
  // pass before the next phase's build begins. Empty/omitted = single-phase.
  phases?: LoopBuildPhase[];

  // Planning bindings — only meaningful when planning is true.
  plan_agent_id?: string;
  plan_skill?: string;
  plan_model?: string;
  plan_thinking?: string;

  // Status names — optional, server defaults apply (todo / in_progress /
  // in_review / done) when omitted.
  planning_status?: string;
  build_status?: string;
  review_status?: string;
  done_status?: string;

  // Spec-wide judge fallback, used only by a judge check without its own
  // assignee_id.
  judge_agent_id?: string;
  judge_skill?: string;
  judge_model?: string;
  judge_thinking?: string;
}

export const DEFAULT_LOOP_CAPS: LoopCaps = {
  max_iterations: 6,
  max_revisions: 6,
  no_progress_stalls: 2,
};

// GET /{id}/loop-state response — the control strip's live read.
export interface LoopStateResponse {
  round: number;
  max_iterations?: number;
  stopped: boolean;
  stop_reason?: string;
  pending_human_checks: PendingHumanCheck[];
}

export interface PendingHumanCheck {
  check_id: string;
  prompt: string;
  assignee_type: LoopAssigneeType;
  assignee_id: string;
}

// FIR-2283 v2 point 8 — "per-issue workflow activation".
// POST /{id}/activate response.
export interface ActivateWorkflowResponse {
  activated: boolean;
  workflow_id: string;
  issue_id: string;
}

// GET /for-issue/{issueId} response — which recipe (if any) an issue is
// currently running. workflow_id is only present when active is true.
export interface ActiveWorkflowForIssueResponse {
  active: boolean;
  workflow_id?: string;
}

export interface CerebroWorkflowRun {
  id: string;
  workflow_id: string;
  workspace_id: string;
  target_issue_id?: string;
  task_id?: string;
  status: CerebroWorkflowRunStatus;
  attempt: number;
  error?: string;
  started_at?: string;
  finished_at?: string;
  next_retry_at?: string;
  created_at: string;
}

export interface WorkflowsListResponse {
  workflows: CerebroWorkflow[];
}

export interface WorkflowRunsListResponse {
  runs: CerebroWorkflowRun[];
  limit: number;
  offset: number;
}

// FIR-2283 v2 point 7 — one issue that has run through a recipe's loop, for
// the run-history page's issue log. Derived server-side from the generated
// child rows; no separate run table.
export interface IssueLoopRun {
  issue_id: string;
  issue_number: number;
  issue_title: string;
  issue_status: string;
  first_activated_at: string;
  last_activated_at: string;
}

export interface IssueLoopRunsResponse {
  issue_runs: IssueLoopRun[];
}

// Phase-3 regenerate-endpoint responses (JEH-1108). The plaintext secret /
// token leaves the server exactly once, on the regenerate response; the
// UI is responsible for showing it to the user before navigating away.
export interface RegenerateInboundTokenResponse {
  inbound_webhook_token: string;
  inbound_webhook_url: string;
}

export interface RegenerateInboundSigningSecretResponse {
  inbound_signing_secret: string;
}

export interface RegenerateOutboundSecretResponse {
  outbound_webhook_secret: string;
}

// Form input shape used by the workflow-form view. Maps 1:1 onto the
// `writeWorkflowRequest` Go type on the server side.
export interface WorkflowWriteInput {
  name: string;
  enabled?: boolean;
  project_id?: string;
  // trigger_type/action_type are required for a "standard" workflow. For an
  // issue_loop write, omit them entirely — the server pins an inert
  // placeholder and compiles the real rules from loop_spec instead (see
  // issueLoopPlaceholderRule on the server).
  trigger_type?: CerebroWorkflowTriggerType;
  trigger_config?: unknown;
  conditions?: unknown;
  action_type?: CerebroWorkflowActionType;
  action_config?: unknown;
  // Phase 2: optional. Omitting editor_mode keeps the server-side default of
  // "form" — form-mode workflows therefore don't need to send these fields.
  editor_mode?: CerebroWorkflowEditorMode;
  editor_layout?: unknown;
  // FIR-2283. Omit for a standard workflow.
  workflow_type?: CerebroWorkflowType;
  loop_spec?: LoopSpec;
}

// Display metadata for the form builder. Order here is the order shown in
// the trigger / action dropdowns.
export const TRIGGER_OPTIONS: ReadonlyArray<{
  value: CerebroWorkflowTriggerType;
  label: string;
  description: string;
}> = [
  {
    value: "status_changed",
    label: "Status changed",
    description: "Fires when an issue moves between statuses.",
  },
  {
    value: "due_date_reached",
    label: "Due date reached",
    description: "Fires when an issue's due date arrives.",
  },
  {
    value: "due_time_reached",
    label: "Due time reached",
    description: "Fires at a specific clock time on the due date.",
  },
  {
    value: "cron",
    label: "Cron schedule",
    description: "Fires on a cron-style schedule (e.g. every weekday at 09:00).",
  },
  {
    value: "webhook_inbound",
    label: "Inbound webhook",
    description: "Fires when an external system POSTs to this workflow's webhook URL.",
  },
  {
    value: "comment_mention",
    label: "Comment mention",
    description: "Fires when a new comment matches an agent/member/keyword/regex.",
  },
  {
    value: "all_children_done",
    label: "All children done",
    description: "Fires when every sub-issue of a parent reaches done.",
  },
  {
    value: "sub_issue_created",
    label: "Sub-issue created",
    description: "Fires when a new sub-issue is added under any (or a specific) parent.",
  },
];

export const ACTION_OPTIONS: ReadonlyArray<{
  value: CerebroWorkflowActionType;
  label: string;
  description: string;
}> = [
  {
    value: "set_status",
    label: "Set status",
    description: "Change the issue's status to a chosen value.",
  },
  {
    value: "create_sub_issue",
    label: "Create sub-issue",
    description: "Create a child issue under the triggered issue.",
  },
  {
    value: "send_reminder",
    label: "Send reminder",
    description: "Write an inbox notification to a member or agent.",
  },
  {
    value: "run_skill",
    label: "Run skill",
    description: "Enqueue an agent task that runs a named skill with input.",
  },
  {
    value: "comment_on_issue",
    label: "Comment on issue",
    description: "Post a comment on the triggered issue or its parent.",
  },
  {
    value: "route_by_domain",
    label: "Route by domain",
    description:
      "Klassificér issuet (kode/business/design/indhold) og attach et `<prefix><domain>` label.",
  },
  {
    value: "escalate_to_owner",
    label: "Escalate to owner",
    description:
      "Walks the parent-issue chain and posts a comment on the first ancestor with an assignee, if the stalled issue is older than the threshold.",
  },
  {
    value: "webhook_outbound",
    label: "Outbound webhook",
    description: "POST the trigger event (and optional issue snapshot) to an external URL.",
  },
  {
    value: "reassign_issue",
    label: "Reassign issue",
    description: "Re-point the triggered issue's assignee to a member or agent.",
  },
];

export const RUN_STATUS_BADGE: Record<CerebroWorkflowRunStatus, string> = {
  queued: "bg-muted text-muted-foreground",
  running: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  success: "bg-green-500/10 text-green-600 dark:text-green-400",
  failed: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  escalated: "bg-red-500/10 text-red-600 dark:text-red-400",
};

// Outbound webhook headers — the server-side guard rejects anything that
// doesn't match this allow-ish list. Kept in sync so the UI can show
// inline errors before the user clicks save.
export const FORBIDDEN_OUTBOUND_HEADERS: ReadonlyArray<string> = [
  "authorization",
  "cookie",
];

export const FORBIDDEN_OUTBOUND_HEADER_PREFIXES: ReadonlyArray<string> = [
  "x-multica-",
];

export const HEADER_NAME_RE = /^[A-Za-z0-9-]+$/;

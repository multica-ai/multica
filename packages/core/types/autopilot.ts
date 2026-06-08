export type AutopilotStatus = "active" | "paused" | "archived";

export type AutopilotExecutionMode = "create_issue" | "run_only";

// `assignee_type` selects which polymorphic actor backs the autopilot:
// "agent" → assignee_id references agent(id); "squad" → assignee_id references
// squad(id) and dispatch resolves to squad.leader_id at run time (MUL-2429,
// Path A). Older servers omit this field — callers should default to "agent".
export type AutopilotAssigneeType = "agent" | "squad";

export type AutopilotTriggerKind = "schedule" | "webhook" | "api";

// `skipped` is emitted by the backend pre-flight admission check
// (assignee runtime offline at dispatch time, MUL-1899). The frontend MUST
// handle it explicitly — falling through to a generic case used to show
// the run as still-pending which masked the no-op.
//
// `pending_runtime` (MUL-2863) is the durable-dispatch outcome: the cron
// fired but the assignee agent's runtime is offline, so the run is parked
// with status='pending_runtime' and pending_runtime_id set. The
// runtime-comes-online hook on the server drains the queue and dispatches
// the parked run at the next opportunity. The frontend surfaces this as
// "Waiting for agent runtime to come online" so the user knows the run
// is durable, not lost.
export type AutopilotRunStatus =
  | "issue_created"
  | "running"
  | "pending_runtime"
  | "completed"
  | "failed"
  | "skipped";

export type AutopilotRunSource = "schedule" | "manual" | "webhook" | "api";

export interface Autopilot {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  project_id?: string | null;
  assignee_type: AutopilotAssigneeType;
  assignee_id: string;
  status: AutopilotStatus;
  execution_mode: AutopilotExecutionMode;
  issue_title_template: string | null;
  created_by_type: string;
  created_by_id: string;
  last_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface WebhookEventFilter {
  event: string;
  actions?: string[];
}

export interface AutopilotTrigger {
  id: string;
  autopilot_id: string;
  kind: AutopilotTriggerKind;
  enabled: boolean;
  cron_expression: string | null;
  timezone: string | null;
  next_run_at: string | null;
  webhook_token: string | null;
  // webhook_path is computed server-side from webhook_token (always
  // "/api/webhooks/autopilots/{token}"). Optional so older servers can be
  // talked to gracefully.
  webhook_path?: string | null;
  // webhook_url is only present when MULTICA_PUBLIC_URL is configured
  // server-side. Clients fall back to composing from getBaseUrl/origin +
  // webhook_path when this is missing.
  webhook_url?: string | null;
  label: string | null;
  // event_filters is only present for webhook triggers. Null/empty means
  // "accept all events".
  event_filters?: WebhookEventFilter[] | null;
  last_fired_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface AutopilotRun {
  id: string;
  autopilot_id: string;
  trigger_id: string | null;
  source: AutopilotRunSource;
  status: AutopilotRunStatus;
  issue_id: string | null;
  task_id: string | null;
  triggered_at: string;
  completed_at: string | null;
  failure_reason: string | null;
  // pending_runtime_id is the runtime the run is parked behind when
  // status='pending_runtime' (MUL-2863). Null for every other status;
  // cleared on the runtime-comes-online re-dispatch. The frontend uses
  // this to render "Waiting for runtime <name>" alongside the failure
  // reason in the run history.
  pending_runtime_id?: string | null;
  trigger_payload: unknown;
  result: unknown;
  created_at: string;
}

export interface CreateAutopilotRequest {
  title: string;
  description?: string;
  project_id?: string | null;
  // Optional on the wire — when omitted the server defaults to "agent" so
  // older clients keep working.
  assignee_type?: AutopilotAssigneeType;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
  issue_title_template?: string;
}

export interface UpdateAutopilotRequest {
  title?: string;
  description?: string | null;
  project_id?: string | null;
  // Send `assignee_type` together with `assignee_id` whenever you change the
  // assignee — the server requires both for a type swap.
  assignee_type?: AutopilotAssigneeType;
  assignee_id?: string;
  status?: AutopilotStatus;
  execution_mode?: AutopilotExecutionMode;
  issue_title_template?: string | null;
}

export interface CreateAutopilotTriggerRequest {
  kind: AutopilotTriggerKind;
  cron_expression?: string;
  timezone?: string;
  label?: string;
  // event_filters is only meaningful for webhook triggers.
  event_filters?: WebhookEventFilter[];
}

export interface UpdateAutopilotTriggerRequest {
  enabled?: boolean;
  cron_expression?: string;
  timezone?: string;
  label?: string;
  // event_filters is only meaningful for webhook triggers.
  event_filters?: WebhookEventFilter[] | null;
}

export interface ListAutopilotsResponse {
  autopilots: Autopilot[];
  total: number;
}

export interface GetAutopilotResponse {
  autopilot: Autopilot;
  triggers: AutopilotTrigger[];
}

export interface ListAutopilotRunsResponse {
  runs: AutopilotRun[];
  total: number;
}

// Webhook delivery enum is server-canonical. The frontend MUST `default`
// any switch on it to a generic fallback — see API Response Compatibility
// rules in CLAUDE.md. PR1 collapsed `skipped` into `dispatched` (the run
// itself carries the skip state); a future server may add new values.
export type WebhookDeliveryStatus =
  | "queued"
  | "dispatched"
  | "rejected"
  | "ignored"
  | "failed";

export type WebhookSignatureStatus =
  | "not_required"
  | "valid"
  | "invalid"
  | "missing";

export interface WebhookDelivery {
  id: string;
  workspace_id: string;
  autopilot_id: string;
  trigger_id: string;
  provider: string;
  event: string;
  dedupe_key: string | null;
  dedupe_source: string | null;
  signature_status: WebhookSignatureStatus;
  status: WebhookDeliveryStatus;
  attempt_count: number;
  content_type: string | null;
  response_status: number | null;
  autopilot_run_id: string | null;
  replayed_from_delivery_id: string | null;
  error: string | null;
  received_at: string;
  last_attempt_at: string;
  created_at: string;
  // Detail-only fields. The list endpoint omits these to keep the wire
  // size bounded (raw_body alone can be up to 256 KiB per delivery).
  selected_headers?: Record<string, unknown> | null;
  raw_body?: string | null;
  response_body?: string | null;
}

export interface ListWebhookDeliveriesResponse {
  deliveries: WebhookDelivery[];
  total: number;
}

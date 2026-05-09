export type AutopilotStatus = "active" | "paused" | "archived";

export type AutopilotExecutionMode = "create_issue" | "run_only";

export type AutopilotTriggerKind = "schedule" | "webhook" | "api";

// `skipped` is emitted by the backend pre-flight admission check
// (assignee runtime offline at dispatch time, MUL-1899). The frontend MUST
// handle it explicitly — falling through to a generic case used to show
// the run as still-pending which masked the no-op.
export type AutopilotRunStatus =
  | "issue_created"
  | "running"
  | "completed"
  | "failed"
  | "skipped";

export type AutopilotRunSource = "schedule" | "manual" | "webhook" | "api";

export interface Autopilot {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
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
  trigger_payload: unknown;
  result: unknown;
  created_at: string;
}

export interface CreateAutopilotRequest {
  title: string;
  description?: string;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
  issue_title_template?: string;
}

export interface UpdateAutopilotRequest {
  title?: string;
  description?: string | null;
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
}

export interface UpdateAutopilotTriggerRequest {
  enabled?: boolean;
  cron_expression?: string;
  timezone?: string;
  label?: string;
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

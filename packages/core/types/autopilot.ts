export type AutopilotStatus = "active" | "paused" | "archived";

export type AutopilotExecutionMode = "create_issue" | "run_only";

// `assignee_type` selects which polymorphic actor backs the autopilot:
// "agent" → assignee_id references agent(id); "squad" → assignee_id references
// squad(id) and dispatch resolves to squad.leader_id at run time (MUL-2429,
// Path A). Older servers omit this field — callers should default to "agent".
export type AutopilotAssigneeType = "agent" | "squad";

export type AutopilotTriggerKind = "schedule" | "webhook" | "api";

export type AutopilotRunStatus = "issue_created" | "running" | "skipped" | "completed" | "failed";

export type AutopilotRunSource = "schedule" | "manual" | "webhook" | "api";

export interface Autopilot {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
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
  // CEREBRO-PATCH(private-autopilot-types): owner-only autopilot visibility flag (JEH-1749).
  is_private: boolean;
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
  // Optional on the wire — when omitted the server defaults to "agent" so
  // older clients keep working.
  assignee_type?: AutopilotAssigneeType;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
  issue_title_template?: string;
  // CEREBRO-PATCH(private-autopilot-request-types): allow UI/CLI clients to set privacy (JEH-1749).
  is_private?: boolean;
}

export interface UpdateAutopilotRequest {
  title?: string;
  description?: string | null;
  // Send `assignee_type` together with `assignee_id` whenever you change the
  // assignee — the server requires both for a type swap.
  assignee_type?: AutopilotAssigneeType;
  assignee_id?: string;
  status?: AutopilotStatus;
  execution_mode?: AutopilotExecutionMode;
  issue_title_template?: string | null;
  // CEREBRO-PATCH(private-autopilot-request-types): allow UI/CLI clients to update privacy (JEH-1749).
  is_private?: boolean;
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

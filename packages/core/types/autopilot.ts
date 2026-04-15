export type AutopilotStatus = "active" | "paused" | "archived";

export type AutopilotExecutionMode = "create_issue" | "run_only";

export type AutopilotTriggerKind = "schedule" | "webhook" | "api";

export type AutopilotRunStatus = "issue_created" | "running" | "completed" | "failed";

export type AutopilotRunSource = "schedule" | "manual" | "webhook" | "api";

export interface Autopilot {
  id: string;
  workspace_id: string;
  project_id: string | null;
  title: string;
  description: string | null;
  assignee_id: string;
  priority: string;
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
  project_id?: string;
  priority?: string;
  execution_mode: AutopilotExecutionMode;
  issue_title_template?: string;
}

export interface UpdateAutopilotRequest {
  title?: string;
  description?: string | null;
  assignee_id?: string;
  project_id?: string | null;
  priority?: string;
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

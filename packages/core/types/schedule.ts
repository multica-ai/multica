export interface Schedule {
  id: string;
  workspace_id: string;
  created_by: string;
  name: string;
  title_template: string;
  description: string;
  assignee_type: "agent" | "member";
  assignee_id: string;
  priority: string;
  cron_expression: string;
  timezone: string;
  enabled: boolean;
  next_run_at: string;
  last_run_at: string | null;
  last_run_issue_id: string | null;
  last_run_error: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateScheduleRequest {
  name: string;
  title_template: string;
  description?: string;
  assignee_type: "agent" | "member";
  assignee_id: string;
  priority?: string;
  cron_expression: string;
  timezone?: string;
  enabled?: boolean;
}

export interface UpdateScheduleRequest {
  name?: string;
  title_template?: string;
  description?: string;
  assignee_type?: "agent" | "member";
  assignee_id?: string;
  priority?: string;
  cron_expression?: string;
  timezone?: string;
  enabled?: boolean;
}

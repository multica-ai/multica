// FIR-2666 project sprint feature — shared types.

export type CadenceUnit = "day" | "week" | "month";
export const CADENCE_UNITS: CadenceUnit[] = ["day", "week", "month"];

export type SprintStatus = "planned" | "active" | "done" | "cancelled";
export const SPRINT_STATUSES: SprintStatus[] = ["planned", "active", "done", "cancelled"];

export interface SprintSettings {
  project_id: string;
  workspace_id: string;
  enabled: boolean;
  duration_unit: CadenceUnit;
  duration_count: number;
  /** ISO weekday: 1=Mon..7=Sun. Only meaningful when duration_unit=week. */
  start_weekday: number;
  /** Name template — {n} is replaced with the sprint sequence number. */
  name_template: string;
  auto_create_enabled: boolean;
  /** "X days before" — the setting Jesper explicitly called out as not-hardcoded. */
  auto_create_lead_days: number;
  move_incomplete_enabled: boolean;
  timezone: string;
  created_at: string;
  updated_at: string;
}

export interface SprintSettingsWriteInput {
  enabled?: boolean;
  duration_unit?: CadenceUnit;
  duration_count?: number;
  start_weekday?: number;
  name_template?: string;
  auto_create_enabled?: boolean;
  auto_create_lead_days?: number;
  move_incomplete_enabled?: boolean;
  timezone?: string;
}

export interface Sprint {
  id: string;
  workspace_id: string;
  project_id: string;
  name: string;
  sequence_no: number;
  status: SprintStatus;
  /** YYYY-MM-DD */
  start_date: string;
  /** YYYY-MM-DD */
  end_date: string;
  goal?: string;
  created_at: string;
  updated_at: string;
}

export interface SprintsListResponse {
  sprints: Sprint[];
}

export interface SprintCreateInput {
  name: string;
  start_date: string;
  end_date: string;
  status?: SprintStatus;
  goal?: string;
}

export interface SprintUpdateInput {
  name?: string;
  start_date?: string;
  end_date?: string;
  status?: SprintStatus;
  goal?: string;
}

export interface SprintIssueLink {
  issue_id: string;
  sprint_id: string;
  added_at: string;
}

export interface SprintIssuesResponse {
  issues: SprintIssueLink[];
}

export interface RecurringTask {
  id: string;
  workspace_id: string;
  project_id: string;
  cadence_unit: CadenceUnit;
  cadence_count: number;
  title: string;
  description?: string;
  priority?: string;
  assignee_type?: "member" | "agent";
  assignee_id?: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface RecurringTasksResponse {
  recurring_tasks: RecurringTask[];
}

export interface RecurringTaskWriteInput {
  cadence_unit: CadenceUnit;
  cadence_count: number;
  title: string;
  description?: string;
  priority?: string;
  assignee_type?: "member" | "agent";
  assignee_id?: string;
  enabled?: boolean;
}

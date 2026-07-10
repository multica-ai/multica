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
  move_incomplete_target_status: string;
  timezone: string;
  /**
   * FIR-1657: when true, issues from OTHER projects in the same workspace may
   * be assigned to this project's sprints. Default false keeps the project
   * closed until its owner opts in.
   */
  accepts_external_issues: boolean;
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
  move_incomplete_target_status?: string;
  timezone?: string;
  accepts_external_issues?: boolean;
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

/**
 * FIR-1657: one option in an issue's sprint picker. A sprint the issue may be
 * assigned to, annotated with its owning project's title and whether that
 * project is the issue's own home project (so the picker can group them as
 * "This project" vs "Other projects").
 */
export interface SelectableSprint extends Sprint {
  project_title: string;
  is_own_project: boolean;
}

export interface SelectableSprintsResponse {
  sprints: SelectableSprint[];
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

// FIR-2828: how to handle the issues still assigned to a sprint when it is
// completed. "leave" keeps them on the now-done sprint, "backlog" unassigns
// them and moves their status to backlog, "move_to_sprint" reassigns them to
// another open sprint in the same project (target_sprint_id required).
export type IncompleteIssuesAction = "leave" | "backlog" | "move_to_sprint";

export interface CompleteSprintInput {
  incomplete_issues_action: IncompleteIssuesAction;
  target_sprint_id?: string;
}

export interface CompleteSprintResponse {
  sprint: Sprint;
  issues_moved: number;
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
  sprint_day_offset: number;
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
  sprint_day_offset?: number;
  enabled?: boolean;
}

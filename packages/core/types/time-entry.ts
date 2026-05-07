export type TimeEntrySyncStatus =
  | "pending"
  | "synced"
  | "failed"
  | "not_linked";

export interface TimeEntry {
  id: string;
  issue_id: string;
  user_id: string;
  duration_minutes: number;
  activity_name: string | null;
  redmine_activity_id: number | null;
  comment: string;
  spent_on: string;
  external_time_entry_id: string | null;
  sync_status: TimeEntrySyncStatus;
  timer_started_at: string | null;
  timer_stopped_at: string | null;
  created_at: string;
}

export interface RedmineActivity {
  id: number;
  name: string;
  is_default: boolean;
}

export interface CreateTimeEntryRequest {
  duration_minutes: number;
  redmine_activity_id?: number;
  activity_name?: string;
  comment?: string;
  spent_on?: string;
  timer_started_at?: string;
  timer_stopped_at?: string;
}

export interface ListTimeEntriesResponse {
  time_entries: TimeEntry[];
  total_minutes: number;
  redmine_task_total_minutes?: number;
}

export interface UpdateTimeEntryRequest {
  duration_minutes?: number;
  redmine_activity_id?: number;
  activity_name?: string;
  comment?: string;
  spent_on?: string;
}

export interface BulkRetryResponse {
  retried: number;
  succeeded: number;
  failed: number;
}

export interface DashboardEntry {
  id: string;
  issue_id: string;
  issue_number: number;
  issue_title: string;
  duration_minutes: number;
  activity_name: string | null;
  comment: string;
  spent_on: string;
  sync_status: TimeEntrySyncStatus;
  created_at: string;
}

export interface DashboardResponse {
  start_date: string;
  end_date: string;
  total_minutes: number;
  daily: { date: string; total_minutes: number }[];
  by_activity: { activity: string; total_minutes: number }[];
  by_issue: {
    issue_id: string;
    issue_number: number;
    issue_title: string;
    total_minutes: number;
  }[];
  entries: DashboardEntry[];
}

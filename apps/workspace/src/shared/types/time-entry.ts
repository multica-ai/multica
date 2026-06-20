// Time entry type definition for the live timer feature

export interface TimeEntryLabel {
  id: string;
  workspace_id: string;
  name: string;
  color: string;
}

export interface TimeEntry {
  id: string;
  workspace_id: string;
  user_id: string;
  /** Linked issue ID (nullable). */
	  issue_id: string | null;
  description: string | null;
  start_time: string;
  /** Null while the timer is running. */
  stop_time: string | null;
  /**
   * Negative while running (= -start_time.Unix()), positive when stopped (seconds).
   * Elapsed seconds = Date.now() / 1000 + duration_seconds when duration_seconds < 0.
   */
  duration_seconds: number;
  /** Entry type: "manual" (default) or "pomodoro" (auto-created by the pomodoro work-phase complete). */
  type: string;
  /** Workspace-scoped labels attached to the time entry. */
  labels?: TimeEntryLabel[];
  created_at: string;
  updated_at: string;
}

export interface CreateTimeEntryRequest {
  description?: string;
  issue_id?: string | null;
  label_ids?: string[];
  start_time: string;
  /** Omit to start a live timer. */
  stop_time?: string;
  confirm_overlap?: boolean;
}

export interface SwitchTimeEntryRequest {
  description?: string;
  issue_id?: string | null;
  label_ids?: string[];
  start_time: string;
}

export interface UpdateTimeEntryRequest {
  description?: string;
  issue_id?: string | null;
  label_ids?: string[];
  /** ISO 8601. Only for stopped entries. */
  start_time?: string;
  /** ISO 8601. Only for stopped entries. */
  stop_time?: string;
  confirm_overlap?: boolean;
}

export interface TimeEntryOverlapConflict {
  id: string;
  description: string | null;
  start_time: string;
  stop_time: string | null;
  issue_id: string | null;
}

export interface TimeEntryOverlapErrorPayload {
  error: string;
  code: "time_entry_overlap";
  conflicts: TimeEntryOverlapConflict[];
}

export interface ListTimeEntriesResponse {
  entries: TimeEntry[];
}

// ── Team time stats ────────────────────────────────────────────────────────────

/** Aggregated time for one workspace member within a date range. */
export interface TeamTimeUserStat {
  user_id: string;
  total_seconds: number;
}

/** Aggregated time per project within a date range. project_id is null for unlinked entries. */
export interface TeamTimeProjectStat {
  project_id: string | null;
  total_seconds: number;
}

/** Response from GET /api/time-entries/team-stats */
export interface TeamTimeStats {
  since: string;
  until: string;
  by_user: TeamTimeUserStat[];
  by_project: TeamTimeProjectStat[];
}

// Time entry type definition for the live timer feature

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
  created_at: string;
  updated_at: string;
}

export interface CreateTimeEntryRequest {
  description?: string;
  issue_id?: string | null;
  start_time: string;
  /** Omit to start a live timer. */
  stop_time?: string;
}

export interface UpdateTimeEntryRequest {
  description?: string;
  issue_id?: string | null;
  /** ISO 8601. Only for stopped entries. */
  start_time?: string;
  /** ISO 8601. Only for stopped entries. */
  stop_time?: string;
}

export interface ListTimeEntriesResponse {
  entries: TimeEntry[];
}

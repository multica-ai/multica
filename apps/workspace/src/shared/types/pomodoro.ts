// Pomodoro session types for server-driven timer state.

export type PomodoroPhase = "work" | "break" | "long_break";
export type PomodoroStatus = "idle" | "running" | "paused";

export interface PomodoroSession {
  id?: string;
  user_id?: string;
  workspace_id?: string;
  phase: PomodoroPhase;
  phase_duration_seconds: number;
  status: PomodoroStatus;
  elapsed_seconds: number;
  /** ISO 8601 timestamp — set when status transitions to "running", null otherwise. */
  started_at: string | null;
  /** Cumulative completed work pomodoros in the current cycle. */
  pomodoro_count?: number;
  created_at?: string;
  updated_at?: string;
}

/** Body accepted by the complete-phase endpoint. */
export interface CompletePomodoroBody {
  issue_id?: string;
  note?: string;
  /** Override the long-break cycle threshold for this completion. */
  long_break_after?: number;
}

/** Response returned by the complete-phase endpoint. */
export interface CompletePomodoroResponse {
  session: PomodoroSession;
  /** The phase that will start next: "break" | "long_break" | "work". */
  next_phase: PomodoroPhase;
}

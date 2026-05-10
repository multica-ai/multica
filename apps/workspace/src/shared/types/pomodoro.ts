// Pomodoro session types for server-driven timer state.

export type PomodoroPhase = "work" | "break";
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
  created_at?: string;
  updated_at?: string;
}

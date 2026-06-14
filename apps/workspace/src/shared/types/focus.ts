import type { TimeEntry } from "./time-entry";

export type FocusMode = "pomodoro" | "flowtime" | "quick_start";
export type FocusPhase =
  | "idle"
  | "focusing"
  | "paused"
  | "break_suggested"
  | "breaking"
  | "completed"
  | "abandoned";

export type FocusReason =
  | "unclear_next_step"
  | "too_large"
  | "low_energy"
  | "avoidance"
  | "interruption"
  | "blocked"
  | "urgent_work"
  | "not_needed"
  | "other";

export interface FocusSession {
  id?: string;
  workspace_id?: string;
  user_id?: string;
  mode: FocusMode;
  phase: FocusPhase;
  preset?: string;
	  issue_id?: string | null;
	  plan_item_id?: string | null;
	  description?: string | null;
  commitment_text?: string | null;
  label_ids: string[];
  first_started_at?: string | null;
  started_at?: string | null;
  paused_at?: string | null;
  elapsed_focus_seconds: number;
  suggested_break_seconds?: number | null;
  status_reason?: string | null;
  reason_note?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface FocusEvent {
  id: string;
  workspace_id: string;
  user_id: string;
  focus_session_id?: string | null;
  event_type: string;
  reason?: string | null;
  note?: string | null;
  duration_seconds?: number | null;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface FocusSessionResponse {
  session: FocusSession;
}

export interface FocusMutationResponse extends FocusSessionResponse {
  event?: FocusEvent;
}

export interface FocusCompleteResponse extends FocusMutationResponse {
  time_entry?: TimeEntry;
  next_action?: "start_break";
}

export interface FocusEventsResponse {
  events: FocusEvent[];
}

export interface StartFocusRequest {
  mode: FocusMode;
  preset?: string;
	  issue_id?: string | null;
	  plan_item_id?: string | null;
	  description?: string;
  commitment_text?: string;
  label_ids?: string[];
  timer_conflict_action?: "stop_existing" | "convert_existing" | "cancel";
  resistance_reason?: FocusReason;
  resistance_note?: string;
}

export interface UpdateFocusRequest {
  issue_id?: string | null;
  description?: string;
  commitment_text?: string;
  label_ids?: string[];
}

export interface FocusReasonRequest {
  reason?: FocusReason;
  note?: string;
}

export interface CompleteFocusRequest {
	  note?: string;
	  end_reason?: "completed" | "stopped_early";
	  plan_item_status_after_complete?: "progressed" | "done";
	}

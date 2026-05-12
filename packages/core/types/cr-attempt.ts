export type CROutcome =
  | "completed_clean"
  | "completed_with_findings"
  | "silent_partial"
  | "silent_total"
  | "failed"
  | "skipped";

export type CRSignalKind =
  | "check_run"
  | "issue_comment"
  | "review_comment"
  | "review"
  | "thread";

export interface CRAttempt {
  id: string;
  issue_id: string;
  workspace_id: string;
  cr_round: number;
  pr_url: string;
  head_sha: string;
  started_at: string;
  review_submitted_at: string | null;
  review_state: string | null;
  findings_count: number;
  outcome: CROutcome | null;
  outcome_reason: string | null;
  closed_at: string | null;
  first_signal_at: string | null;
  first_signal_kind: CRSignalKind | null;
}

export interface CRSignal {
  id: string;
  attempt_id: string;
  signal_kind: CRSignalKind;
  signal_action: string | null;
  received_at: string;
  payload_summary: Record<string, unknown> | null;
}

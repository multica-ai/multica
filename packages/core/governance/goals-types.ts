/**
 * Goals, key results and metrics (spec 4.1). The server computes progress and
 * confidence from the readings; this client only shows them. Dates are
 * `YYYY-MM-DD`; timestamps are RFC 3339.
 */

export const GOAL_SCOPE_TYPES = ["company", "floor", "squad", "initiative"] as const;
export type GoalScopeType = (typeof GOAL_SCOPE_TYPES)[number];

export const GOAL_STATUSES = ["draft", "active", "achieved", "missed", "dropped"] as const;
export type GoalStatus = (typeof GOAL_STATUSES)[number] | "unknown";

/** Confidence a goal is judged at. `unknown` is shown when a key result has no reading yet. */
export const GOAL_CONFIDENCES = ["on_track", "at_risk", "off_track", "unknown"] as const;
export type GoalConfidence = (typeof GOAL_CONFIDENCES)[number];

/** The confidence an owner may set by hand. */
export const GOAL_OVERRIDE_CONFIDENCES = ["on_track", "at_risk", "off_track"] as const;
export type GoalOverrideConfidence = (typeof GOAL_OVERRIDE_CONFIDENCES)[number];

export const METRIC_SOURCES = ["builtin", "manual", "webhook"] as const;
export type MetricSource = (typeof METRIC_SOURCES)[number] | "unknown";

export interface Metric {
  key: string;
  name: string;
  unit: string;
  source: MetricSource;
  direction: "increase" | "decrease";
  description: string;
  created_at: string;
}

export interface MetricReading {
  id: string;
  metric_key: string;
  scope_type: GoalScopeType;
  scope_id: string | null;
  value: number;
  observed_at: string;
  actor_type: string;
  actor_id: string | null;
  note: string;
}

export interface GoalSeriesPoint {
  observed_at: string;
  value: number;
}

export interface KeyResult {
  id: string;
  goal_id: string;
  title: string;
  metric_key: string;
  scope_type: GoalScopeType;
  scope_id: string | null;
  start_value: number;
  target_value: number;
  weight: number;
  /** The latest reading in the goal's period, or null when there is none. */
  current_value: number | null;
  observed_at: string | null;
  /** 0 to 1, or null when there is no reading. */
  progress: number | null;
  /** Readings in the period, oldest first. Empty in a list. */
  series: GoalSeriesPoint[];
}

export interface Goal {
  id: string;
  workspace_id: string;
  scope_type: GoalScopeType;
  scope_id: string | null;
  parent_goal_id: string | null;
  title: string;
  why: string;
  period_start: string;
  period_end: string;
  owner_type: "squad" | "member" | "agent";
  owner_id: string;
  status: GoalStatus;
  /** The confidence in effect: the owner's override when one is set. */
  confidence: GoalConfidence;
  /** The confidence the readings give. */
  derived_confidence: GoalConfidence;
  confidence_override: GoalOverrideConfidence | null;
  confidence_note: string;
  confidence_changed_at: string;
  /** 0 to 1, or null when no key result has a reading. */
  progress: number | null;
  readings_missing: number;
  source_proposal_id: string | null;
  key_results: KeyResult[];
  created_by_type: string;
  created_by_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface GoalRef {
  id: string;
  title: string;
  status: GoalStatus;
  scope_type: GoalScopeType;
  scope_id: string | null;
}

export interface GoalDetail {
  goal: Goal;
  child_goals: GoalRef[];
}

export interface GoalFilters {
  scope_type?: GoalScopeType;
  scope_id?: string;
  status?: GoalStatus;
}

export interface KeyResultInput {
  title: string;
  metric_key: string;
  /** Empty reads the metric at the goal's scope. */
  scope_type?: GoalScopeType;
  scope_id?: string | null;
  start_value: number;
  target_value: number;
  weight?: number;
}

export interface CreateGoalRequest {
  scope_type: Exclude<GoalScopeType, "squad">;
  scope_id?: string | null;
  parent_goal_id?: string | null;
  title: string;
  why?: string;
  period_start: string;
  period_end: string;
  owner_type: "squad" | "member" | "agent";
  owner_id: string;
  key_results: KeyResultInput[];
}

/** A change to a goal; absent fields keep their value. */
export interface UpdateGoalRequest {
  title?: string;
  why?: string;
  period_start?: string;
  period_end?: string;
  owner_type?: "squad" | "member" | "agent";
  owner_id?: string;
  parent_goal_id?: string | null;
  key_results?: KeyResultInput[];
}

export interface ReportReadingRequest {
  scope_type: GoalScopeType;
  scope_id?: string | null;
  value: number;
  observed_at?: string;
  note?: string;
  /** Only `webhook` is sent by this client; a manual report leaves it out. */
  source?: "manual" | "webhook";
  name?: string;
  unit?: string;
  direction?: "increase" | "decrease";
}

export interface MetricReadingFilters {
  scope_type?: GoalScopeType;
  scope_id?: string | null;
  since?: string;
  limit?: number;
}

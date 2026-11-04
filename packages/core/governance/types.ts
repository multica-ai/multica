/**
 * Whether the company is working. `quiet` is set by hand or by the daily quiet
 * window. `unknown` stands in for a state a newer server adds, or for a
 * response this client could not read: views show it as-is and never claim
 * the company is running.
 */
export type CompanyState = "running" | "paused" | "quiet" | "unknown";

/** The states a person can choose. `unknown` is never a choice. */
export type SelectableCompanyState = Exclude<CompanyState, "unknown">;

export interface QuietHours {
  enabled: boolean;
  /** `HH:MM` in the workspace timezone. */
  start: string;
  /** `HH:MM` in the workspace timezone. */
  end: string;
  /** An IANA timezone name, such as `Europe/London`. */
  timezone: string;
}

export interface CompanyStateInfo {
  /** The state in force now: a quiet window shows as `quiet` while it lasts. */
  state: CompanyState;
  /** The state the boss chose. It differs from `state` only while a quiet window is in force. */
  setting: CompanyState;
  quiet_hours: QuietHours;
  updated_at: string;
}

export interface SetCompanyStateRequest {
  state: SelectableCompanyState;
  /** Only with `paused`: also cancels the running routine and delegated tasks. */
  stop_running?: boolean;
}

/** Event kinds the governance log knows. A newer server kind reads as `unknown`. */
export const GOVERNANCE_EVENT_KINDS = [
  "policy_auto_approve",
  "policy_created",
  "policy_revoked",
  "budget_set",
  "budget_extended",
  "budget_hold",
  "budget_release",
  "autonomy_changed",
  "org_change_applied",
  "org_change_reverted",
  "auto_publish",
  "initiative_forced",
  "company_paused",
  "company_resumed",
  "goal_activated",
  "goal_imported",
  "review_published",
] as const;

export type GovernanceEventKind = (typeof GOVERNANCE_EVENT_KINDS)[number] | "unknown";

export const GOVERNANCE_ACTOR_TYPES = ["member", "agent", "system", "policy"] as const;

export type GovernanceActorType = (typeof GOVERNANCE_ACTOR_TYPES)[number] | "unknown";

/**
 * Subject types the log filters by. The server writes `subject_type` as free
 * text, so a type not in this list still shows (as its raw value) and is
 * simply not offered as a filter.
 */
export const GOVERNANCE_SUBJECT_TYPES = [
  "company",
  "floor",
  "squad",
  "agent",
  "initiative",
  "budget",
  "approval_policy",
  "proposal",
  "goal",
  "review",
] as const;

export type GovernanceSubjectType = (typeof GOVERNANCE_SUBJECT_TYPES)[number];

export interface GovernanceEvent {
  id: string;
  kind: GovernanceEventKind;
  actor_type: GovernanceActorType;
  actor_id: string | null;
  subject_type: string;
  subject_id: string | null;
  summary: string;
  details: Record<string, unknown>;
  created_at: string;
}

export interface GovernanceEventFilters {
  kind?: GovernanceEventKind;
  actor_type?: GovernanceActorType;
  subject_type?: string;
}

export interface GovernanceEventPage {
  events: GovernanceEvent[];
  /** Pass to the next request as `cursor`; null when the log is exhausted. */
  next_cursor: string | null;
}

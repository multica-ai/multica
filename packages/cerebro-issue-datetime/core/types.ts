// Optional time-of-day attached to an issue's start_date / due_date, plus the
// per-issue choice of whether the assigned agent auto-starts on one of those
// dates. The dates themselves stay pure calendar days on the issue; this is the
// companion time. Times are wall-clock "HH:MM" (24h) strings, or null when unset.

export type IssueTimeKind = "start" | "due";

// Whether (and when) an agent-assigned issue auto-starts. "none" = never;
// "start"/"due" = start the assigned agent when that date+time arrives. null on
// IssueDateTimes means the issue inherits the workspace default.
export type AgentStartKind = "none" | "start" | "due";

export interface IssueDateTimes {
  start_time: string | null;
  due_time: string | null;
  agent_start_kind: AgentStartKind | null;
}

export const EMPTY_ISSUE_DATE_TIMES: IssueDateTimes = {
  start_time: null,
  due_time: null,
  agent_start_kind: null,
};

// Workspace-wide default for new issues, set under Settings. Always a concrete
// value (never null) — the server applies "none" when no row exists.
export interface WorkspaceDateTimeSettings {
  default_agent_start_kind: AgentStartKind;
}

export const DEFAULT_WORKSPACE_DATE_TIME_SETTINGS: WorkspaceDateTimeSettings = {
  default_agent_start_kind: "none",
};

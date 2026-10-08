export interface IssueWakeup {
  id: string;
  issue_id: string;
  agent_id: string;
  agent_name: string;
  instruction: string;
  kind: "event" | "at" | "every" | "cron";
  mode: "once" | "continuous";
  event_types: string[];
  filter_agent_id: string | null;
  filter_task_id: string | null;
  filter_actor_type?: "member" | "agent" | null;
  filter_actor_id?: string | null;
  filter_actor_name?: string | null;
  interval_seconds: number | null;
  cron_expression: string | null;
  timezone: string;
  next_fire_at: string | null;
  enabled: boolean;
  revision?: number;
  disabled_at: string | null;
  last_task_id: string | null;
  last_error: string | null;
  filter_agent_name?: string | null;
  last_task_status?: string | null;
  /** When the rule ends if nothing triggered it first. */
  expires_at?: string | null;
  /** Set for relative waits: re-enabling restarts the wait from now. */
  expiry_seconds?: number | null;
  on_timeout?: "wake" | "end" | null;
  /** The deadline, not a trigger or a person, ended the rule. */
  timed_out_at?: string | null;
  /** True when an agent run created the rule on behalf of created_by_name. */
  created_by_agent?: boolean;
  created_by_name?: string | null;
  source_agent_id?: string | null;
  source_agent_name?: string | null;
  /** A fact the platform checks itself; null for event and time rules. */
  condition?: WakeupCondition | null;
  /** Repeating rules stop after this many runs. */
  max_fires?: number | null;
  fire_count?: number;
  /** Why the platform, not a person, stopped the rule. */
  paused_reason?: WakeupPausedReason | null;
}

export type WakeupPausedReason = "max_fires" | "loop" | "rate";

/** Structured predicates the platform evaluates (see WakeupCondition in Go). */
export type WakeupCondition =
  | { type: "issue_field"; field: "status"; value: string }
  | { type: "issue_field"; field: "assignee"; assignee_type: "member" | "agent" | "squad"; assignee_id: string }
  | { type: "issue_field"; field: "label"; label_id: string }
  | { type: "issue_field"; field: "property"; property_id: string; value: unknown }
  | { type: "children_done"; stage?: number | null }
  | { type: "pull_request"; event: "checks_finished" | "merged" }
  | { type: "other_issue"; issue_id: string; state: "done" | "ended" | "in_review"; identifier?: string };

/** One run a rule started, for its trigger history. */
export interface WakeupRun {
  id: string;
  status: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  /** Set when a scheduled check ended silently. */
  checkin_note: string;
  triggers: string[];
  commented: boolean;
}

/** A rule the platform paused on an open issue. */
export interface PausedWakeup {
  issue_id: string;
  id: string;
  agent_id: string;
  paused_reason: WakeupPausedReason;
}

/** Body accepted by POST /api/issues/:id/wakeups. */
export interface IssueWakeupInput {
  agent_id: string;
  instruction: string;
  kind: IssueWakeup["kind"];
  mode?: IssueWakeup["mode"];
  event_types?: string[];
  filter_agent_id?: string;
  filter_actor_type?: "member" | "agent";
  filter_actor_id?: string;
  at?: string;
  interval_seconds?: number;
  cron_expression?: string;
  timezone?: string;
  expires_at?: string;
  expires_in_seconds?: number;
  on_timeout?: "wake" | "end";
  condition?: WakeupCondition;
  max_fires?: number;
}

/**
 * A platform-defined wakeup on one issue. `child_done` wakes the parent's
 * assignee when a stage of its sub-issues closes while a later one waits, and
 * once more when every sub-issue is closed.
 */
export interface SystemWakeup {
  /** Empty until the rule exists on the issue (first sub-issue change or edit). */
  id: string;
  revision: number;
  rule: "child_done";
  enabled: boolean;
  /** Set on this issue; runs get `default_instruction` when it is empty. */
  instruction: string;
  default_instruction: string;
  /** A person changed the rule on this issue; it no longer follows the default. */
  customized: boolean;
  paused_reason: WakeupPausedReason | null;
  /** True while a stage is open; otherwise the rule waits for every sub-issue. */
  staged: boolean;
  stage: number | null;
  total: number;
  remaining: number;
  waiting: string[];
  target: { type: "agent" | "squad" | "member"; id: string; name: string } | null;
  /** Why no run would start now; a member assignee gets an inbox notification. */
  blocked: "" | "backlog" | "member_assignee" | "no_assignee";
  /** The workspace-wide setting, which applies until the issue sets its own. */
  workspace_default: boolean;
}

/** A platform rule's workspace default, edited in Settings. */
export interface WorkspaceSystemWakeup {
  rule: "child_done";
  enabled: boolean;
  /** The workspace's instruction; empty means runs get `builtin_instruction`. */
  instruction: string;
  builtin_instruction: string;
  /** Open issues whose rule a person changed; they ignore this default. */
  customized: number;
}

export type WakeupPreview = Pick<
  IssueWakeup,
  | "id"
  | "issue_id"
  | "agent_id"
  | "agent_name"
  | "kind"
  | "mode"
  | "event_types"
  | "filter_task_id"
  | "filter_agent_name"
  | "filter_actor_type"
  | "filter_actor_id"
  | "filter_actor_name"
  | "interval_seconds"
  | "cron_expression"
  | "timezone"
  | "next_fire_at"
  | "condition"
>;
export interface IssueWakeupSummaryRow extends WakeupPreview {
  active_count: number;
  event_count: number;
}

export type WakeupScope = "active" | "all" | "paused" | "disabled" | "ended";
export type WakeupSource = "member" | "agent" | "system";
export interface WorkspaceWakeup extends Omit<IssueWakeup, "instruction"> {
  issue_title: string;
  issue_identifier: string;
  issue_closed: boolean;
  can_manage: boolean;
  active_runs: number;
  task: import("./agent").AgentTask | null;
  source: WakeupSource;
  /** Runs the rule started in the last seven days. */
  runs_7d: number;
  /** Set on a system rule row, whose id is its issue's id. */
  rule?: SystemWakeup["rule"] | null;
  system_stage?: number | null;
  system_remaining?: number | null;
  target_type?: string | null;
}
export interface WorkspaceWakeupPage {
  items: WorkspaceWakeup[];
  total: number;
  counts: Record<WakeupScope, number>;
  agents: { id: string; name: string }[];
}
export interface WorkspaceWakeupFilters {
  scope: WakeupScope;
  kind: "all" | "event" | "at" | "recurring";
  source: "" | WakeupSource;
  search: string;
  agent_id: string;
  offset: number;
  limit: number;
}

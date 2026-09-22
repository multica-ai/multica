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
>;
export interface IssueWakeupSummaryRow extends WakeupPreview {
  active_count: number;
  event_count: number;
}

export type WakeupScope = "active" | "all" | "disabled" | "ended";
export interface WorkspaceWakeup extends Omit<IssueWakeup, "instruction"> {
  issue_title: string;
  issue_identifier: string;
  issue_closed: boolean;
  can_manage: boolean;
  active_runs: number;
  task: import("./agent").AgentTask | null;
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
  search: string;
  agent_id: string;
  offset: number;
  limit: number;
}

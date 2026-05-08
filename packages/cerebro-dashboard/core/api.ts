import { api } from "@multica/core/api";

export type TimeRange = "24h" | "7d" | "30d";

export interface Kpi {
  value: number;
  prior: number;
}

export interface Bucket {
  key: string;
  count: number;
}

export interface DayBucket {
  day: string;
  issues_created: number;
  issues_done: number;
  tasks_completed: number;
  tasks_failed: number;
}

export interface TopActor {
  id: string;
  name: string;
  count: number;
}

export interface ActivityEntry {
  id: string;
  issue_id?: string;
  issue_title?: string;
  issue_number?: number;
  issue_kind?: string;
  actor_type: string;
  actor_id: string;
  action: string;
  details?: unknown;
  created_at: string;
}

export interface RecentTask {
  task_id: string;
  agent_id: string;
  agent_name: string;
  agent_avatar_url?: string;
  issue_id?: string;
  issue_title?: string;
  issue_number?: number;
  status: string;
  completed_at?: string;
  started_at?: string;
  created_at: string;
}

export interface DashboardOverview {
  range: TimeRange;
  period_start: string;
  period_end: string;
  issues_created: Kpi;
  issues_completed: Kpi;
  chat_messages: Kpi;
  channel_messages: Kpi;
  channels_active: Kpi;
  tasks_completed: Kpi;
  tasks_failed: Kpi;
  agents_active: Kpi;
  members_active: Kpi;
  spend_cents: Kpi;
  issues_by_status: Bucket[];
  issues_by_priority: Bucket[];
  timeline: DayBucket[];
  top_agents: TopActor[];
  top_members: TopActor[];
  activity_feed: ActivityEntry[];
  recent_tasks: RecentTask[];
}

export async function fetchDashboardOverview(
  range: TimeRange,
): Promise<DashboardOverview> {
  return api.getCerebroDashboardOverview<DashboardOverview>(range);
}

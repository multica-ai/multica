import { api } from "@multica/core/api";
import type { ActorScope } from "./types";

export type TimeRange = "24h" | "7d" | "30d";
export type ActorType = "member" | "agent";

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
  spend_cents?: number;
}

export interface ActivityEntry {
  id: string;
  issue_id?: string;
  issue_title?: string;
  issue_number?: number;
  issue_kind?: string;
  actor_type: string;
  actor_id: string;
  actor_name: string;
  action: string;
  details?: unknown;
  created_at: string;
}

export interface MessageFlowEntry {
  sender_id: string;
  sender_name: string;
  recipient_id: string;
  recipient_name: string;
  count: number;
}

export interface RecentTask {
  task_id: string;
  agent_id: string;
  agent_name: string;
  agent_avatar_url?: string;
  task_title?: string;
  issue_id?: string;
  issue_title?: string;
  issue_number?: number;
  chat_session_id?: string;
  status: string;
  completed_at?: string;
  started_at?: string;
  created_at: string;
}

export interface ActorMessage {
  id: string;
  content: string;
  created_at: string;
  sender_id?: string;
  sender_name?: string;
  agent_id: string;
  agent_name: string;
  session_id: string;
  issue_id?: string;
  issue_number?: number;
  issue_title?: string;
}

export interface ActorMessagesResponse {
  actor_id: string;
  range: TimeRange;
  messages: ActorMessage[];
}

export interface AllMessagesResponse {
  range: TimeRange;
  messages: ActorMessage[];
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
  issues_by_on_behalf_of: TopActor[];
  timeline: DayBucket[];
  top_agents: TopActor[];
  top_members: TopActor[];
  top_message_senders: TopActor[];
  top_message_recipients: TopActor[];
  message_flow: MessageFlowEntry[];
  activity_feed: ActivityEntry[];
  recent_tasks: RecentTask[];
}

export async function fetchDashboardOverview(
  range: TimeRange,
  scope: ActorScope,
  actorId: string | null,
): Promise<DashboardOverview> {
  const actorType = scopeToActorType(scope);
  return api.getCerebroDashboardOverview<DashboardOverview>(range, {
    actor_type: actorType,
    actor_id: actorType ? actorId : null,
  });
}

export async function fetchActorMessages(
  actorId: string,
  range: TimeRange,
): Promise<ActorMessagesResponse> {
  return api.getCerebroDashboardActorMessages<ActorMessagesResponse>(actorId, range);
}

export async function fetchAllMessages(range: TimeRange): Promise<AllMessagesResponse> {
  return api.getCerebroDashboardAllMessages<AllMessagesResponse>(range);
}

function scopeToActorType(scope: ActorScope): ActorType | undefined {
  if (scope === "members") return "member";
  if (scope === "agents") return "agent";
  return undefined;
}

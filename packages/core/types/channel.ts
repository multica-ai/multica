import type { AgentTask } from "./agent";

export interface ChannelSummary {
  id: string;
  workspace_id: string;
  name: string;
  slug: string;
  description: string;
  access_mode: "open" | "invite";
  is_locked: boolean;
  is_archived: boolean;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  is_member: boolean;
  member_role?: string;
  has_unread: boolean;
  last_activity_at?: string;
}

export interface ListChannelsResponse {
  channels: ChannelSummary[];
  total: number;
}

export interface ChannelMember {
  channel_id: string;
  user_id: string;
  role: string;
  last_read_at: string;
  joined_at: string;
  user_name: string;
  user_email: string;
  user_avatar_url: string | null;
}

export interface ListChannelMembersResponse {
  members: ChannelMember[];
  total: number;
}

export interface ChannelThreadSummary {
  id: string;
  channel_id: string;
  workspace_id: string;
  title: string;
  created_by: string | null;
  creator_name?: string;
  creator_avatar_url?: string;
  message_count: number;
  issue_count: number;
  last_message_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListChannelThreadsResponse {
  threads: ChannelThreadSummary[];
  total: number;
}

export type ChannelAgentTask = AgentTask & {
  channel_id: string;
  channel_message_id: string;
  channel_thread_id?: string;
  channel_reply_to_id?: string;
  agent_name?: string;
};

export interface ChannelMessage {
  id: string;
  thread_id?: string;
  channel_id: string;
  workspace_id: string;
  author_type: "member" | "agent" | "system";
  author_id: string | null;
  author_name?: string;
  author_avatar_url?: string;
  content: string;
  reply_to_id?: string;
  reply_count?: number;
  created_at: string;
  updated_at: string;
  agent_tasks?: ChannelAgentTask[];
}

export interface ListChannelMessagesResponse {
  messages: ChannelMessage[];
  total: number;
}

export interface MessageThreadResponse {
  root_message: ChannelMessage;
  replies: ChannelMessage[];
  thread: ChannelThreadSummary | null;
}

export interface ChannelContextMessage {
  id: string;
  author_type: string;
  author_id: string | null;
  author_name?: string;
  content: string;
  created_at: string;
}

export interface ChannelContextResponse {
  channel: ChannelSummary;
  members: ChannelMember[];
  messages: ChannelContextMessage[];
}

export interface ThreadLinkedIssue {
  id: string;
  number: number;
  title: string;
  status: string;
  identifier?: string;
}

export interface ListThreadMessagesResponse {
  thread: ChannelThreadSummary;
  messages: ChannelMessage[];
  issues: ThreadLinkedIssue[];
}

export interface CreateChannelRequest {
  name: string;
  description?: string;
  access_mode?: "open" | "invite";
}

export interface UpdateChannelRequest {
  name?: string;
  description?: string;
  access_mode?: "open" | "invite";
  is_locked?: boolean;
  is_archived?: boolean;
}

export interface CreateChannelThreadRequest {
  title?: string;
  content?: string;
}

export interface CreateChannelMessageRequest {
  content: string;
}

export interface ConvertMessageToIssueRequest {
  title?: string;
  description?: string;
  project_id?: string;
}

export interface ConvertMessageToIssueResponse {
  issue_id: string;
  issue_number: number;
  title: string;
}

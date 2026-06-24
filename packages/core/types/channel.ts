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
  // First top-level message newer than the member's last_read_at. Drives the
  // "jump to last read" landing point on channel entry. Null when the user is
  // not a member or has no unread.
  first_unread_message_id?: string | null;
  last_activity_at?: string;
  group_id?: string | null;
  group_name?: string | null;
  group_position: number;
  position: number;
}

export interface ChannelGroup {
  id: string;
  name: string;
  position: number;
  created_by: string | null;
  created_at: string;
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
  issues?: ThreadLinkedIssue[];
}

export interface ListChannelMessagesResponse {
  messages: ChannelMessage[];
  total: number;
  has_more?: boolean;
  // Present only when ?around targeted a reply: the window is centered on
  // root_message_id (a top-level message), and the client should auto-expand
  // thread_id and scroll-highlight message_id (the original reply).
  highlight?: {
    root_message_id: string;
    thread_id: string;
    message_id: string;
  };
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

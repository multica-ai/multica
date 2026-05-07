// Channels are issues with kind in ('channel', 'dm'). The frontend treats
// them as a separate concept because the UI differs — title is the channel
// name, subscribers are participants, comments are messages.

export type ChannelKind = "channel" | "dm";

export interface ChannelMember {
  user_type: "member" | "agent";
  user_id: string;
}

export interface ChannelLastMessage {
  author_type: "member" | "agent";
  author_id: string;
  content: string;
  created_at: string;
}

export interface Channel {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  kind: ChannelKind;
  title: string;
  description: string | null;
  status: string;
  project_id: string | null;
  assignee_type: string | null;
  assignee_id: string | null;
  creator_type: string;
  creator_id: string;
  participants: ChannelMember[];
  unread_count: number;
  last_message: ChannelLastMessage | null;
  created_at: string;
  updated_at: string;
}

export interface CreateChannelRequest {
  kind: ChannelKind;
  name: string;
  description?: string;
  project_id?: string | null;
  member_ids: string[];
  agent_ids: string[];
}

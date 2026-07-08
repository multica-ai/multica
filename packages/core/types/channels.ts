export interface Channel {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  is_private: boolean;
  created_by: string;
  created_at: string;
}

export interface CreateChannelRequest {
  name: string;
  description?: string;
  is_private: boolean;
}

export interface UpdateChannelRequest {
  name?: string;
  description?: string;
  is_private?: boolean;
}

export interface ChannelMember {
  id: string;
  user_id: string;
  role: string;
  joined_at: string;
}

export interface ChannelMessage {
  id: string;
  channel_id: string;
  author_id: string;
  content: string;
  parent_id?: string;
  edited_at?: string;
  created_at: string;
}

export interface CreateChannelMessageRequest {
  content: string;
  parent_id?: string;
}

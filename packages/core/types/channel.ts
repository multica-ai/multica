export interface Channel {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  lark_chat_id: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ChannelMember {
  member_type: "user" | "agent";
  member_id: string;
  name: string;
  created_at: string;
}

export interface ChannelMessage {
  id: string;
  channel_id: string;
  workspace_id: string;
  author_type: "user" | "agent" | "lark" | "system";
  author_id: string | null;
  author_name: string;
  content: string;
  source: "multica" | "lark";
  external_message_id: string | null;
  created_at: string;
}

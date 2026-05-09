export interface Channel {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  type: "public" | "private" | "dm";
  created_by: string;
  is_member: boolean;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ChannelMember {
  id: string;
  channel_id: string;
  member_type: "user" | "agent";
  member_id: string;
  role: "owner" | "admin" | "member";
  name: string;
  avatar_url?: string;
  joined_at: string;
}

export interface ChannelMessage {
  id: string;
  channel_id: string;
  author_type: "user" | "agent" | "system";
  author_id: string;
  content: string;
  thread_root_id: string | null;
  task_id: string | null;
  status: "active" | "converted" | "deleted";
  edited_at: string | null;
  created_at: string;
}

export interface CreateChannelRequest {
  name: string;
  description?: string;
  type?: "public" | "private" | "dm";
}

export interface AddMemberRequest {
  member_type: "user" | "agent";
  member_id: string;
  role?: "owner" | "admin" | "member";
}

export interface SendChannelMessageRequest {
  content: string;
  thread_root_id?: string;
}

export interface ChannelReadState {
  channel_id: string;
  member_type: string;
  member_id: string;
  last_read_message_id: string | null;
  last_read_at: string;
}

/**
 * Proposal system message for channel → issue conversion.
 * Stored in channel_message.metadata when status = "converted".
 */
export interface ChannelProposalMetadata {
  type: "proposal";
  status: "consensus_ready" | "converted";
  marked_by: string;
  body: {
    problem: string;
    scope: string[];
    open_questions: string[];
    acceptance: string;
  };
  converted_issue_id: string | null;
}

/**
 * Team conversations: channels where people and agents talk, member-to-member
 * direct messages, and the threads under a message of either. Agents reply in
 * a channel when @mentioned; their replies arrive as `author_type: "agent"`.
 */

export type ConversationKind = "channel" | "direct" | "thread";
export type ConversationVisibility = "public" | "private";
export type ConversationMemberType = "member" | "agent";
export type ConversationAuthorType = "member" | "agent" | "system";

export interface ConversationMember {
  member_type: ConversationMemberType;
  member_id: string;
}

export interface ConversationWorkingAgent {
  agent_id: string;
  task_id: string;
}

export interface ConversationThreadSummary {
  id: string;
  reply_count: number;
  last_reply_at: string | null;
}

export interface ConversationMessage {
  id: string;
  conversation_id: string;
  seq: number;
  author_type: ConversationAuthorType;
  author_id: string | null;
  /** `event` is a system line, e.g. why an agent did not reply. */
  kind: "message" | "event";
  content: string;
  task_id: string | null;
  client_message_id: string | null;
  created_at: string;
  thread?: ConversationThreadSummary;
}

export interface Conversation {
  id: string;
  workspace_id: string;
  kind: ConversationKind;
  name: string;
  about: string;
  visibility: ConversationVisibility;
  parent_conversation_id?: string | null;
  root_message_id?: string | null;
  created_by: string;
  last_message_seq: number;
  last_message_at: string | null;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
  is_member: boolean;
  last_read_seq: number;
  unread_count: number;
  members: ConversationMember[];
  working_agents?: ConversationWorkingAgent[];
  root_message?: ConversationMessage;
}

export interface ConversationMessagesPage {
  messages: ConversationMessage[];
  has_more: boolean;
}

export interface CreateChannelRequest {
  name: string;
  about?: string;
  visibility: ConversationVisibility;
  members?: ConversationMember[];
}

export interface ConversationEventPayload {
  conversation_id: string;
  parent_conversation_id?: string;
  message?: ConversationMessage;
}

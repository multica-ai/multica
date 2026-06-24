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
  // CEREBRO-PATCH(core-types-channel-unread-smart): FIR-2010 — true when the
  // channel has any unread message, even when unread_count is mention-only
  // (smart unread on). Optional: absent on older servers → treated as false.
  has_unread_activity?: boolean;
  last_message: ChannelLastMessage | null;
  created_at: string;
  updated_at: string;
}

// CEREBRO-PATCH(core-types-channel-perms): TECH-3698 — per-channel permission settings.
export type ChannelRenamePolicy = "admins" | "everyone";
export type ChannelAddMembersPolicy = "admins" | "everyone";
export interface ChannelPermissions {
  rename_policy: ChannelRenamePolicy;
  add_members_policy: ChannelAddMembersPolicy;
  allow_self_leave: boolean;
}

export interface CreateChannelRequest {
  kind: ChannelKind;
  name: string;
  description?: string;
  project_id?: string | null;
  member_ids: string[];
  agent_ids: string[];
  // CEREBRO-PATCH(core-types-channel-perms): TECH-3698 — creator-chosen settings (optional; server defaults when omitted).
  rename_policy?: ChannelRenamePolicy;
  add_members_policy?: ChannelAddMembersPolicy;
  allow_self_leave?: boolean;
}

// CEREBRO-PATCH(core-types-channel-listen): JEH-699 — per (channel ×
// agent) listen-mode toggle. 'always' (default) means the agent reacts
// to every comment in the channel; 'mention_only' falls back to the
// upstream behaviour of only triggering on explicit @-mention.
export type ChannelAgentListenMode = "always" | "mention_only";

export interface ChannelAgentSetting {
  agent_id: string;
  listen_mode: ChannelAgentListenMode;
}

export interface ChannelAgentSettingsResponse {
  settings: ChannelAgentSetting[];
}

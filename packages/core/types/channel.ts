// TypeScript counterparts to db.Channel / db.ChannelMembership /
// db.ChannelMessage (server/pkg/db/generated/models.go), shaped for the
// JSON the handlers serialize. Keep these in sync with the Go response
// types in server/internal/handler/channel.go and channel_message.go.

export type ChannelKind = "channel" | "dm";
export type ChannelVisibility = "public" | "private";
export type ChannelActorType = "member" | "agent";
export type ChannelMemberRole = "member" | "admin";
export type ChannelNotificationLevel = "all" | "mentions" | "none";

export interface Channel {
  id: string;
  workspace_id: string;
  /** Slug for human channels; deterministic hash for DMs. */
  name: string;
  /** Human-readable name; empty for DMs (UI builds from membership). */
  display_name: string;
  description: string;
  kind: ChannelKind;
  visibility: ChannelVisibility;
  created_by_type: ChannelActorType;
  created_by_id: string;
  /** Per-channel retention override in days. null = inherit workspace default. */
  retention_days: number | null;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
  /** Per-actor unread state. Populated by ListChannels (sidebar uses it for
   * the badge + first-unread anchor); 0 / null on detail/get endpoints. */
  unread_count: number;
  last_read_message_id: string | null;
}

export interface ChannelMembership {
  channel_id: string;
  member_type: ChannelActorType;
  member_id: string;
  role: ChannelMemberRole;
  notification_level: ChannelNotificationLevel;
  joined_at: string;
  last_read_message_id: string | null;
  last_read_at: string | null;
}

export interface ChannelReaction {
  id: string;
  channel_message_id: string;
  actor_type: ChannelActorType;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export interface ChannelMessageAttachment {
  id: string;
  workspace_id: string;
  filename: string;
  url: string;
  download_url: string;
  content_type: string;
  size_bytes: number;
  uploader_type: ChannelActorType;
  uploader_id: string;
  created_at: string;
}

export interface ChannelMessage {
  id: string;
  channel_id: string;
  author_type: ChannelActorType;
  author_id: string;
  content: string;
  parent_message_id: string | null;
  edited_at: string | null;
  /** Soft-delete timestamp; UI renders "[message deleted]" placeholder. */
  deleted_at: string | null;
  created_at: string;
  /** Phase 4 — reactions hydrated by the list/get/thread endpoints. The
   * create endpoint returns an empty array since reactions can't exist on
   * a brand-new row. */
  reactions: ChannelReaction[];
  /** Phase 4 — count of non-deleted replies under this message.
   * Always 0 for replies themselves (the schema is one level deep in v1). */
  thread_reply_count: number;
  /** Phase 5 — attachments stamped via metadata.attachments at create time
   * and hydrated server-side on read. Order matches what the author selected
   * in the composer. */
  attachments: ChannelMessageAttachment[];
}

export interface ChannelMessageThread {
  parent: ChannelMessage;
  replies: ChannelMessage[];
}

/** Phase 5c — full-text search hit. Carries enough channel context that
 * results can render with a "#channel" prefix without an extra fetch. */
export interface ChannelSearchHit extends ChannelMessage {
  channel_name: string;
  channel_display_name: string;
  channel_kind: ChannelKind;
  rank: number;
}

// --- Request payloads ----------------------------------------------------

export interface CreateChannelRequest {
  name: string;
  display_name?: string;
  description?: string;
  visibility?: ChannelVisibility; // default "public"
  retention_days?: number | null;
}

export interface UpdateChannelRequest {
  display_name?: string;
  description?: string;
  visibility?: ChannelVisibility;
  retention_days?: number | null;
  /** Set to true alongside retention_days=null to clear the override. */
  retention_days_set?: boolean;
}

export interface AddChannelMemberRequest {
  member_type: ChannelActorType;
  member_id: string;
  role?: ChannelMemberRole;
  notification_level?: ChannelNotificationLevel;
}

export interface CreateChannelMessageRequest {
  content: string;
  parent_message_id?: string | null;
  /** Phase 5 — attachment ids the client uploaded via /api/upload-file
   * before submitting. Stored in channel_message.metadata.attachments. */
  attachment_ids?: string[];
}

export interface MarkChannelReadRequest {
  message_id: string;
}

export interface CreateOrFetchDMRequest {
  participants: { type: ChannelActorType; id: string }[];
}

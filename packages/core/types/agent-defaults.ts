/** Personal or system-level agent default configuration. */
export interface AgentDefaults {
  /** Unique record ID (present when persisted). */
  id?: string;
  /** Free-form configuration object. */
  config: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

/** Agent defaults record with user information (returned by list endpoint). */
export interface AgentDefaultsWithUser extends AgentDefaults {
  user_id: string;
  user_name: string;
  user_avatar_url: string;
}

export type InstructionsHistoryScope = "personal" | "system";

export interface InstructionsHistoryItem {
  id: string;
  workspace_id: string;
  scope: InstructionsHistoryScope;
  member_id?: string | null;
  actor_id?: string | null;
  actor_user_id?: string | null;
  actor_name?: string | null;
  actor_avatar_url?: string | null;
  restored_from?: string | null;
  created_at: string;
  content_preview: string;
}

export interface InstructionsHistoryDetail extends InstructionsHistoryItem {
  content: string;
}

export interface ListInstructionsHistoryResponse {
  items: InstructionsHistoryItem[];
  total: number;
}

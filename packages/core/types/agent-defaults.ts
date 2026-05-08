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

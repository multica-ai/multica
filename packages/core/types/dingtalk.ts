/** A DingTalk robot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `DingTalkInstallationResponse` in
 * `server/internal/handler/dingtalk.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface DingTalkInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
  /** False only when a workspace admin is viewing an orphaned installation
   * whose Agent no longer exists. Optional for older backends. */
  agent_available?: boolean;
  /** DingTalk staff ids linked by the currently authenticated Multica user for
   * this bot. Member-scoped so the member-visible installation endpoint does
   * not disclose other members' DingTalk identities. */
  bound_dingtalk_user_ids?: string[];
}

export interface ListDingTalkInstallationsResponse {
  installations: DingTalkInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable DingTalk" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever DingTalk is
   * configured, i.e. the at-rest key is set — a bring-your-own-app install
   * needs no hosted credentials). Kept as a separate flag for forward/backward
   * compat; optional so an older desktop build that predates it treats it as
   * off. */
  install_supported?: boolean;
}

/** One connected Multica bot observed in a DingTalk group. */
export interface DingTalkGroupBot {
  installation_id: string;
  agent_id: string;
  /** Readable DingTalk bot name. Empty on older backends, transient lookup
   * failures, or until the app is granted qyapi_chat_manage. */
  bot_name: string;
  /** Machine-readable reason the readable identity is unavailable. */
  bot_identity_issue: string;
  /** Latest accepted @mention from this group, in RFC 3339. Optional for
   * compatibility with older backends; empty when no durable message remains. */
  last_active_at?: string;
  /** Number of deduplicated group messages accepted for this bot. */
  mention_count?: number;
}

/** A DingTalk group observed after a validated @bot callback. */
export interface DingTalkGroup {
  conversation_id: string;
  conversation_title: string;
  bots: DingTalkGroupBot[];
}

export interface ListDingTalkGroupsResponse {
  groups: DingTalkGroup[];
  /** False when an installed client is connected to a backend that predates
   * group discovery. Callers use it to avoid polling an absent endpoint. */
  group_discovery_supported: boolean;
  /** Historical observations outside the 90-day active window, keyed by bot
   * installation. Additive for compatibility with older servers. */
  inactive_group_counts?: Record<string, number>;
  /** App-wide identity keyed by installation, available even when every group
   * for that bot is outside the active window. */
  bot_identities?: Record<string, DingTalkGroupBot>;
  /** Offset for the next inactive page. Absent when the page is complete. */
  next_offset?: number;
}

export interface ListDingTalkGroupsParams {
  activity?: "inactive";
  installationId?: string;
  offset?: number;
  limit?: number;
}

/** Request body for a bring-your-own-app (BYO) install: the AppKey and
 * AppSecret an agent owner or workspace admin pastes from the DingTalk
 * Stream-mode robot they created. The backend validates both before persisting,
 * then returns the created DingTalkInstallation. */
export interface RegisterDingTalkBYORequest {
  client_id: string;
  client_secret: string;
}

/** Post-redemption echo: the DingTalk user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemDingTalkBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  dingtalk_user_id: string;
}

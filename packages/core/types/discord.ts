/** A Discord bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `DiscordInstallationResponse` in
 * `server/internal/handler/discord.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface DiscordInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The bot's Discord user id (snowflake). */
  bot_id: string;
  /** The bot's Discord username (without the discriminator). */
  bot_username: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListDiscordInstallationsResponse {
  installations: DiscordInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable Discord" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever Discord is
   * configured — a pasted bot token needs no hosted credential).
   * Optional so an older desktop build that predates it treats it as off. */
  install_supported?: boolean;
}

/** Request body for a bot install: the token the admin copies from the Discord
 * developer portal. The backend validates it live (GET /users/@me) before
 * persisting. */
export interface RegisterDiscordRequest {
  bot_token: string;
}

/** Post-redemption echo: the Discord user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemDiscordBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  discord_user_id: string;
}

/** A Mattermost bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `MattermostInstallationResponse` in
 * `server/internal/handler/mattermost.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface MattermostInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** Canonical base URL of the Mattermost server this bot lives on. Unlike
   * Slack or Telegram, Mattermost is self-hosted, so this is the only thing
   * that tells an admin which deployment a row belongs to. */
  server_url: string;
  /** The bot account's Mattermost user id (unique within its server). */
  bot_user_id: string;
  /** The bot's Mattermost username (without the @). */
  bot_username: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListMattermostInstallationsResponse {
  installations: MattermostInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable Mattermost" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever Mattermost is
   * configured — a pasted server URL and token need no hosted credential).
   * Optional so an older desktop build that predates it treats it as off. */
  install_supported?: boolean;
}

/** Request body for a bot install. Mattermost needs two fields where Slack and
 * Telegram need one: the deployment is the operator's own server, so there is
 * no single API host to assume. The backend validates both live (GET
 * /users/me) before persisting. */
export interface RegisterMattermostRequest {
  server_url: string;
  access_token: string;
}

/** Post-redemption echo: the Mattermost user id the token carried is now bound
 * to the logged-in Multica user in this workspace/installation. */
export interface RedeemMattermostBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  mattermost_user_id: string;
}

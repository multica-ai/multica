/** A Slack bot installation bound to a single Multica agent (MUL-3666).
 *
 * Wire shape mirrors `SlackInstallationResponse` in
 * `server/internal/handler/slack.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface SlackInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The Slack workspace (team) id this bot is installed in. */
  team_id: string;
  /** The installed bot's Slack user id. */
  bot_user_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListSlackInstallationsResponse {
  installations: SlackInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable Slack" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever Slack is configured,
   * i.e. the at-rest key is set — a bring-your-own-app install needs no hosted
   * OAuth credentials). Kept as a separate flag for forward/backward compat;
   * optional so an older desktop build that predates it treats it as off. */
  install_supported?: boolean;
}

/** Request body for a bring-your-own-app (BYO) install: the two tokens the
 * admin pastes from the Slack app they created. The backend validates that both
 * belong to the same Slack app (and that the app token is live) before
 * persisting, then returns the created SlackInstallation. */
export interface RegisterSlackBYORequest {
  bot_token: string;
  app_token: string;
}

/** Post-redemption echo: the Slack user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemSlackBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  slack_user_id: string;
}

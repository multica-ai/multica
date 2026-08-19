/** A Telegram bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `TelegramInstallationResponse` in
 * `server/internal/handler/telegram.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface TelegramInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The bot's numeric Telegram id (the token prefix), as a string. */
  bot_id: string;
  /** The bot's Telegram username (without the @). */
  bot_username: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListTelegramInstallationsResponse {
  installations: TelegramInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable Telegram" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever Telegram is
   * configured — a pasted BotFather token needs no hosted credential).
   * Optional so an older desktop build that predates it treats it as off. */
  install_supported?: boolean;
}

/** Request body for a bot install: the token the admin pastes from
 * @BotFather. The backend validates it live (getMe) before persisting. */
export interface RegisterTelegramRequest {
  bot_token: string;
}

/** Post-redemption echo: the Telegram user id the token carried is now bound
 * to the logged-in Multica user in this workspace/installation. */
export interface RedeemTelegramBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  telegram_user_id: string;
}

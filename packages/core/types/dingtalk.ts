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
  /** The DingTalk app (AppKey / client id) this robot belongs to. */
  app_id: string;
  /** The installed robot's code, used to route outbound messages. */
  robot_code: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
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

/** Request body for a bring-your-own-app (BYO) install: the AppKey and
 * AppSecret the admin pastes from the DingTalk Stream-mode robot they created.
 * The backend validates both before persisting, then returns the created
 * DingTalkInstallation. */
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

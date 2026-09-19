/** A ShareCRM bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `ShareCRMInstallationResponse` in
 * `server/internal/handler/sharecrm.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing the
 * response — see CLAUDE.md → API Compatibility. */
export interface ShareCRMInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListShareCRMInstallationsResponse {
  installations: ShareCRMInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable ShareCRM" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever ShareCRM is
   * configured, i.e. the at-rest key is set — a bring-your-own-app install
   * needs no hosted credentials). Kept as a separate flag for forward/backward
   * compat; optional so an older desktop build that predates it treats it as
   * off. */
  install_supported?: boolean;
}

/** Request body for a bring-your-own-app (BYO) install: the App ID and
 * App Secret the admin pastes from the ShareCRM open-platform bot they created.
 * Optional `gateway_base_url` overrides the public cloud host for private
 * deployments. The backend validates credentials before persisting, then
 * returns the created ShareCRMInstallation. */
export interface RegisterShareCRMBYORequest {
  app_id: string;
  app_secret: string;
  gateway_base_url?: string;
}

/** Post-redemption echo: the ShareCRM user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemShareCRMBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  sharecrm_user_id: string;
}

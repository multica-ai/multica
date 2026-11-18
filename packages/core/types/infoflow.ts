/**
 * An InfoFlow (如流) robot installation bound to a single Multica agent. Wire
 * shape mirrors `InfoFlowInstallationResponse` in
 * `server/internal/handler/infoflow.go`. Any new field the backend adds MUST
 * default to optional so older desktop builds keep parsing the response — see
 * CLAUDE.md → API Compatibility.
 */
export interface InfoFlowInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The robot's AppKey, pasted from the InfoFlow admin console. */
  app_key: string;
  /** Chat display name; absent on older backends and until the first send. */
  bot_name?: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
}

export interface ListInfoFlowInstallationsResponse {
  installations: InfoFlowInstallation[];
  /** Whether MULTICA_INFOFLOW_SECRET_KEY is set on this deployment. When false
   * the BYO Connect button is hidden and the panel renders an "ask the
   * operator" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever configured). Kept as
   * a separate flag for parity with Slack / Lark; optional so a desktop build
   * that predates it treats it as off. */
  install_supported?: boolean;
}

/** Request body for the Web UI's BYO Connect dialog. The two fields are copied
 * from the InfoFlow admin console's robot page: the AppKey and its AppSecret.
 * The backend seals the secret with the deployment's
 * MULTICA_INFOFLOW_SECRET_KEY before writing it, so plaintext never lands in
 * the DB. */
export interface RegisterInfoFlowBYORequest {
  app_key: string;
  app_secret: string;
}

/** Post-redemption echo: the InfoFlow user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemInfoFlowBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  infoflow_user_id: string;
}
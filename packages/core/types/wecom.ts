/**
 * A WeCom smart-bot ("智能机器人" / aibot) installation bound to a single
 * Multica agent. Wire shape mirrors `WecomInstallationResponse` in
 * `server/internal/handler/wecom_web.go`. Any new field the backend adds MUST
 * default to optional so older desktop builds keep parsing the response — see
 * CLAUDE.md → API Compatibility.
 */
export interface WecomInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The smart-bot identifier assigned by the WeCom admin console. */
  bot_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
}

export interface ListWecomInstallationsResponse {
  installations: WecomInstallation[];
  /** Whether MULTICA_WECOM_SECRET_KEY is set on this deployment. When false the
   * retained-installations panel renders an operator notice. */
  configured: boolean;
  /** Whether the legacy install path is available (true whenever configured). Kept as
   * a separate flag for parity with Slack / Lark; optional so a desktop build
   * that predates it treats it as off. Retained for wire compatibility after
   * browser install entry retirement. */
  install_supported?: boolean;
}

/** Post-redemption echo: the WeCom aibot userid the token carried is now
 * bound to the logged-in Multica user in this workspace/installation. */
export interface RedeemWecomBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  wecom_user_id: string;
}

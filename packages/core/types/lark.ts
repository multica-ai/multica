/** A Lark Bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `LarkInstallationResponse` in
 * `server/internal/handler/lark.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing
 * the response — see CLAUDE.md → API Response Compatibility. */
export interface LarkInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  app_id: string;
  tenant_key?: string | null;
  bot_open_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  /** Which Lark cloud the bot lives on: "feishu" (mainland) or "lark"
   * (international). Auto-detected at install time. Optional so an older
   * desktop build parsing a newer server — or a newer build hitting a
   * server that predates the field — defaults to Feishu in the UI
   * (see CLAUDE.md → API Response Compatibility). */
  region?: "feishu" | "lark" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListLarkInstallationsResponse {
  installations: LarkInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When
   * false the retained-installations panel renders an operator notice. */
  configured: boolean;
  /** Whether new installs via the device-flow scan-to-bind path can
   * complete end-to-end — i.e. the device-flow RegistrationService is
   * wired AND the real Lark HTTP APIClient (not the no-op stub) is in
   * place. Retained for wire compatibility after browser install entry
   * points were retired. Optional so older desktop
   * builds receiving a server that does not yet emit the field
   * default to `undefined`, treated as not supported. */
  install_supported?: boolean;
}

export interface RedeemLarkBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  lark_open_id: string;
}

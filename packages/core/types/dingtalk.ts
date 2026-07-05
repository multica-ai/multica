/** A DingTalk bot installation bound to a single Multica agent —
 * created through the scan-to-create device flow ("一键创建钉钉应用").
 *
 * Wire shape mirrors `DingTalkInstallationResponse` in
 * `server/internal/handler/dingtalk_install.go`. New fields the backend
 * adds in the future MUST default to optional so older desktop builds
 * keep parsing the response — see CLAUDE.md → API Compatibility. */
export interface DingTalkInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The DingTalk app's client_id (AppKey) the device flow minted. */
  client_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListDingTalkInstallationsResponse {
  installations: DingTalkInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When
   * false the Bind button must be disabled and the panel renders an
   * empty / "ask the operator to enable DingTalk" state. */
  configured: boolean;
  /** Whether new installs via the scan-to-create device flow can
   * complete end-to-end. When false the install entry points are hidden
   * and the panel surfaces a "coming soon" notice. Optional so older
   * desktop builds receiving a server that does not yet emit the field
   * default to `undefined`, treated as not supported. */
  install_supported?: boolean;
}

/** First half of the device-flow install: the server has opened a
 * registration session against oapi.dingtalk.com and returned the QR
 * URL. The frontend renders `qr_code_url` as a QR (and as a clickable
 * link fallback) and starts polling `/install/{session_id}/status` at
 * the supplied cadence until success or terminal failure. */
export interface BeginDingTalkInstallResponse {
  session_id: string;
  qr_code_url: string;
  expires_in_seconds: number;
  poll_interval_seconds: number;
}

/** Status polling result. `status` is the discriminator. */
export interface DingTalkInstallStatusResponse {
  status: "pending" | "success" | "error" | string;
  /** Populated when status === "success". The frontend invalidates the
   * installations cache so the new row appears in the Settings tab. */
  installation_id?: string;
  /** Stable code on error — switch on this (NOT error_message) to pick
   * the right copy. Common values: "expired", "install_failed",
   * "dingtalk_protocol_error", "credentials_check_failed",
   * "installation_conflict", "internal_error". */
  error_reason?: string;
  /** Human-readable error tail for debugging; the production UI should
   * surface the copy keyed off error_reason and use this only as a
   * diagnostic tooltip. */
  error_message?: string;
}

export interface RedeemDingTalkBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  dingtalk_user_id: string;
}

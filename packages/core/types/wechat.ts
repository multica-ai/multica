/** A WeChat ClawBot (iLink) installation bound to a single Multica agent.
 *
 * Wire shape mirrors `WechatInstallationResponse` in
 * `server/internal/handler/wechat.go`. New fields the backend adds in the
 * future MUST default to optional so older clients keep parsing the
 * response — see CLAUDE.md → API Response Compatibility. */
export interface WechatInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The iLink bot id this installation is addressed by (e.g. "xxxxxx@im.bot").
   * Inbound messages are routed to the installation whose app_id matches the
   * message's to_user_id. */
  app_id: string;
  /** Human-readable id of the WeChat account that scanned the QR code at install
   * time (display only). Optional so older servers that predate the field keep
   * parsing. */
  ilink_user_id?: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListWechatInstallationsResponse {
  installations: WechatInstallation[];
  /** Whether the deployment has the at-rest secret key configured. When false
   * the connect entry points are hidden and the panel renders an "ask the
   * operator to enable WeChat" state. */
  configured: boolean;
  /** Whether the install path is available (true whenever WeChat is configured,
   * i.e. the at-rest key is set — a QR-scan install needs no hosted OAuth
   * credentials). Kept as a separate flag for forward/backward compat; optional
   * so an older client that predates it treats it as off. */
  install_supported?: boolean;
}

/** First half of the QR-login install: the server has fetched a QR code from
 * the iLink backend and returned the URL. The frontend renders `qr_code_url` as
 * a QR (and as a clickable link fallback) and starts polling
 * `/install/{session_id}/status` at the supplied cadence until success or
 * terminal failure. */
export interface BeginWechatInstallResponse {
  session_id: string;
  qr_code_url: string;
  expires_in_seconds: number;
  poll_interval_seconds: number;
}

/** Status polling result. `status` is the discriminator. */
export interface WechatInstallStatusResponse {
  status: "pending" | "success" | "error" | string;
  /** Populated when status === "success". The frontend invalidates the
   * installations cache so the new row appears in the Settings tab. */
  installation_id?: string;
  /** Stable code on error — switch on this (NOT error_message) to pick the
   * right copy. Common values: "expired", "access_denied",
   * "ilink_protocol_error", "installation_conflict", "installer_bind_failed",
   * "session_lost", "internal_error". */
  error_reason?: string;
  /** Human-readable error tail for debugging; the production UI should surface
   * the copy keyed off error_reason and use this only as a diagnostic tooltip. */
  error_message?: string;
}

/** Post-redemption echo: the WeChat user id the token carried is now bound to
 * the logged-in Multica user in this workspace/installation. */
export interface RedeemWechatBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  wechat_user_id: string;
}

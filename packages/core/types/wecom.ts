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
   * BYO Connect button is hidden and the panel renders an "ask the operator"
   * state. */
  configured: boolean;
  /** Whether the install path is available (true whenever configured). Kept as
   * a separate flag for parity with Slack / Lark; optional so a desktop build
   * that predates it treats it as off. */
  install_supported?: boolean;
  /** Whether the QR scan-code install is available. It needs a QR provider on
   * top of the secret key, so it can be off where the BYO path works. Optional
   * so a desktop build that predates it treats it as off and shows only BYO. */
  scan_install_supported?: boolean;
}

/** Request body for the Web UI's BYO Connect dialog. The first two fields are
 * copied from the WeCom admin console's smart-bot page: the bot's stable
 * identifier and its long-connection secret. The backend seals the secret
 * with the deployment's MULTICA_WECOM_SECRET_KEY before writing it, so
 * plaintext never lands in the DB. */
export interface RegisterWecomBYORequest {
  bot_id: string;
  secret: string;
  /** The bot's name as it appears in a chat. Optional, and used for one
   * thing: recognising the bot's own @-mention in a group. WeCom delivers a
   * mention as literal text with no structured mention list, so a name
   * containing a space ("Multica Bot") otherwise swallows the slash command
   * typed after it. Omitting it on a re-install of the same bot keeps the
   * name already stored. */
  bot_name?: string;
}

/** Post-redemption echo: the WeCom aibot userid the token carried is now
 * bound to the logged-in Multica user in this workspace/installation. */
export interface RedeemWecomBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  wecom_user_id: string;
}

/** Response to POST /wecom/install/begin. Nothing exists yet at this point —
 * the QR is fetched out of band by the install worker, so the client polls
 * `session_id` until the status carries one. */
export interface BeginWecomInstallResponse {
  session_id: string;
  status: "creating" | "pending" | "success" | "error" | string;
  /** How long the client should wait before its next status poll. */
  poll_interval_seconds: number;
}

/** One scan-install status poll.
 *
 * `qr_code_url` and `expires_in_seconds` are present only while the status is
 * "pending": there is nothing to render before the QR has been generated, and
 * nothing to render once it has been scanned or has expired. */
export interface WecomInstallStatusResponse {
  status: "creating" | "pending" | "success" | "error" | string;
  qr_code_url?: string;
  expires_in_seconds?: number;
  poll_interval_seconds: number;
  /** Set once the bot is bound, on status === "success". */
  installation_id?: string;
  /** Stable machine code the UI switches on to pick its copy. */
  error_reason?:
    | "expired"
    | "generate_failed"
    | "integration_unconfigured"
    | "installation_conflict"
    | "wecom_protocol_error"
    | "internal_error"
    | string;
  /** Operator-facing detail. Shown only as supplementary text. */
  error_message?: string;
}

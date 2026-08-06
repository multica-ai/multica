/** A WeCom (企业微信) bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `wecomInstallationResponse` in
 * `server/internal/handler/wecom.go`. New fields the backend adds in the
 * future MUST default to optional so older desktop builds keep parsing
 * the response — see CLAUDE.md → API Response Compatibility. */
export interface WecomInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  bot_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListWecomInstallationsResponse {
  installations: WecomInstallation[];
  /** Whether the deployment has MULTICA_WECOM_SECRET_KEY configured. */
  configured: boolean;
  /** Whether new scan-to-bind installs can complete end-to-end. Optional
   * so older desktop builds default to `undefined`, treated as unsupported. */
  install_supported?: boolean;
  /** Whether hand-entered bot credentials can be installed. Needs only the
   * secretbox key, so it can be true while `install_supported` is false on a
   * deployment that never provisioned a WeCom scan source. */
  manual_install_supported?: boolean;
}

/** Begin opens or resumes an install session. The response is always 202;
 * QR and terminal outcomes arrive from status polling — begin never returns
 * `qr_code_url`. */
export interface BeginWecomInstallResponse {
  session_id: string;
  status: "creating" | "pending" | "success" | "error" | string;
  poll_interval_seconds: number;
}

/** Status polling result. QR is present only when status === "pending". */
export interface WecomInstallStatusResponse {
  status: "creating" | "pending" | "success" | "error" | string;
  qr_code_url?: string;
  expires_in_seconds?: number;
  poll_interval_seconds: number;
  installation_id?: string;
  error_reason?:
    | "expired"
    | "generate_failed"
    | "integration_unconfigured"
    | "installation_conflict"
    | "wecom_protocol_error"
    | "internal_error"
    | string;
  error_message?: string;
}

export interface RedeemWecomBindingResponse {
  workspace_id: string;
  installation_id: string;
}

/** Result of connecting an existing bot from hand-entered credentials. The
 * endpoint is synchronous — it verifies the credentials against WeCom before
 * writing — so there is no session to poll. */
export interface ManualWecomInstallResponse {
  installation_id: string;
  bot_id: string;
}

/** Stable error codes `POST /wecom/install/manual` returns in the `error`
 * field. The UI localizes these; it must not pattern-match prose. */
export type ManualWecomInstallErrorCode =
  | "bot_credentials_required"
  | "invalid_bot_credentials"
  | "verify_unavailable"
  | "installation_conflict"
  | "bot_id_owned_by_another_workspace"
  | "bot_id_owned_in_this_workspace"
  | "bot_id_owned_by_archived_agent";

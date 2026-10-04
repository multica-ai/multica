/** A Weixin (WeChat) account relayed to a Multica agent. Each member connects
 * their own Weixin; the relay chats as them.
 *
 * Wire shape mirrors `WeixinInstallationResponse` in
 * `server/internal/handler/weixin.go`. New backend fields must stay optional
 * here so older desktop builds keep parsing — see CLAUDE.md → API
 * Compatibility. */
export interface WeixinInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The Weixin bot account id the relay runs under. */
  account_id: string;
  installer_user_id: string;
  /** "error" while the relay cannot run; `last_error` says why. */
  status: "active" | "error" | string;
  last_error: string | null;
  created_at: string;
}

export interface ListWeixinInstallationsResponse {
  installations: WeixinInstallation[];
  /** A Weixin host is set up on this deployment. */
  configured: boolean;
  /** The host answered, so a new connection can start. */
  install_supported?: boolean;
}

export type WeixinLoginState =
  | "waiting"
  | "scanned"
  | "need_verify_code"
  | "connected"
  | "failed";

/** One QR login. `qr_content` changes when Weixin refreshes an expired code. */
export interface WeixinLogin {
  id: string;
  state: WeixinLoginState | string;
  qr_content: string;
  message: string;
  /** The last verify code the member typed was wrong. */
  verify_code_invalid: boolean;
}

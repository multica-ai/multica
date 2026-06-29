/** A WeChat Work Bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `WechatInstallationResponse` in
 * `server/internal/handler/wechat.go`. */
export interface WechatInstallation {
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

export interface ListWechatInstallationsResponse {
  installations: WechatInstallation[];
  configured: boolean;
}

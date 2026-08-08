export interface WeixinInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  bot_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
}

export interface ListWeixinInstallationsResponse {
  installations: WeixinInstallation[];
  configured: boolean;
  install_supported?: boolean;
}

export interface BeginWeixinInstallResponse {
  session_id: string;
  qr_code_url: string;
  expires_at: string;
}

export interface WeixinInstallStatusResponse {
  status: "waiting" | "scanned" | "confirmed" | "expired" | string;
  installation?: WeixinInstallation;
}

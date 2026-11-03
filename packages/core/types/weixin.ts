export interface WeixinInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  bot_id: string;
  user_id: string;
  installer_user_id: string;
  status: string;
  installed_at: string;
}

export interface ListWeixinInstallationsResponse {
  installations: WeixinInstallation[];
  configured: boolean;
  install_supported?: boolean;
}

export interface WeixinLogin {
  id: string;
  status: string;
  qr_code_url?: string;
  installation_id?: string;
  expires_at: string;
}

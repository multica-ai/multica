export interface WecomInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  bot_id: string;
  corp_id: string;
  self_build_agent_id?: string;
  installer_user_id: string;
  status: string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListWecomInstallationsResponse {
  installations: WecomInstallation[];
  configured: boolean;
}

export interface CreateWecomInstallationRequest {
  agent_id: string;
  bot_id: string;
  bot_secret: string;
  corp_id: string;
  corp_secret: string;
  self_build_agent_id?: string;
}

export interface RedeemWecomBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  wecom_userid: string;
}

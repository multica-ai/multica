/** A ShareCRM IM Gateway bot installation bound to a single Multica agent. */
export interface ShareCRMInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListShareCRMInstallationsResponse {
  installations: ShareCRMInstallation[];
  configured: boolean;
  install_supported?: boolean;
}

export interface RegisterShareCRMBYORequest {
  app_id: string;
  app_secret: string;
  gateway_base_url?: string;
}

export interface RedeemShareCRMBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  sharecrm_user_id: string;
}

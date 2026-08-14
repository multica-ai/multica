/** A personal-polling Qianwen Skill installation bound to one Multica agent. */
export interface QianwenInstallation {
  id: string;
  agent_id: string;
  connection_id: string;
  mode: string;
  status: "active" | "revoked" | string;
  /** Caller-relative. Missing on older backends means unknown, not unbound. */
  current_user_bound?: boolean;
}

export interface ListQianwenInstallationsResponse {
  installations: QianwenInstallation[];
  configured: boolean;
  mode?: string;
  /** Missing on older backends means the pairing capability is unknown. */
  pairing_supported?: boolean;
}

/** Install response. The access token is returned once and must not enter Query cache. */
export interface QianwenInstallResponse extends QianwenInstallation {
  access_token: string;
  token_visible_once: true;
  submit_path: string;
  status_path_pattern: string;
}

/** One-time eight-digit code used to link the caller's Qianwen identity. */
export interface QianwenPairingCodeResponse {
  pairing_code: string;
  expires_at: string;
  code_visible_once: true;
}

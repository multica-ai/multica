/** A personal-polling Qianwen Skill installation bound to one Multica agent. */
export interface QianwenInstallation {
  id: string;
  agentId: string;
  connectionId: string;
  mode: string;
  status: "active" | "revoked" | string;
  /** Caller-relative. Missing on older backends means unknown, not unbound. */
  currentUserBound?: boolean;
}

export interface ListQianwenInstallationsResponse {
  installations: QianwenInstallation[];
  configured: boolean;
  mode?: string;
  /** Missing on older backends means the pairing capability is unknown. */
  pairingSupported?: boolean;
}

/** Install response. The access token is returned once and must not enter Query cache. */
export interface QianwenInstallResponse extends QianwenInstallation {
  accessToken: string;
  tokenVisibleOnce: true;
  submitPath: string;
  statusPathPattern: string;
}

/** One-time eight-digit code used to link the caller's Qianwen identity. */
export interface QianwenPairingCodeResponse {
  pairingCode: string;
  expiresAt: string;
  codeVisibleOnce: true;
}

/** Camel-case agent projection consumed by the Qianwen Settings surface. */
export interface QianwenAgentSummary {
  id: string;
  name: string;
  archivedAt: string | null;
  canManage: boolean;
  canInvoke: boolean;
}

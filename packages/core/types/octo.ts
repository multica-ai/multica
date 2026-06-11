/** An Octo IM bot installation bound to a single Multica agent.
 *
 * Wire shape mirrors `OctoInstallationResponse` in
 * `server/internal/handler/octo.go`. New fields the backend adds in the future
 * MUST default to optional so older desktop builds keep parsing the response —
 * see CLAUDE.md → API Response Compatibility. */
export interface OctoInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  robot_id: string;
  bot_name: string;
  installer_user_id: string;
  status: "active" | "revoked" | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListOctoInstallationsResponse {
  installations: OctoInstallation[];
  /** Whether the deployment has MULTICA_OCTO_SECRET_KEY configured. When false
   * the configure form must be disabled and the panel renders an "ask the
   * operator to enable Octo" state. */
  configured: boolean;
}

/** Response to redeeming an Octo binding token. */
export interface RedeemOctoBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  octo_uid: string;
}

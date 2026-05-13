// Credential governance types — UI-side contract that the JEH-1196 backend
// API is expected to satisfy. When the API lands, validate responses with a
// zod schema in @multica/core/api/schema.ts per the API Response Compatibility
// rule in CLAUDE.md.

export type CredentialType =
  | "repo_deploy_key"
  | "mcp_bearer"
  | "api_key"
  | "gcp_credentials"
  | "webhook_secret"
  | "sso_cert"
  | "oauth_token"
  | "object_storage_key";

export type CredentialStatus = "active" | "expired" | "revoked";

export type Permission =
  | "attach"
  | "read_redacted"
  | "reveal"
  | "rotate"
  | "revoke";

export type SubjectKind = "member" | "agent" | "group";

export interface Credential {
  id: string;
  workspace_id: string;
  type: CredentialType;
  name: string;
  description?: string;
  status: CredentialStatus;
  redacted_value: string;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  last_rotated_at?: string;
}

export interface CredentialPolicy {
  credential_id: string;
  subject_kind: SubjectKind;
  subject_id: string;
  subject_label: string;
  permissions: Permission[];
}

export type AuditOutcome = "allow" | "deny";

export interface AuditEntry {
  id: string;
  credential_id: string;
  actor_kind: SubjectKind;
  actor_id: string;
  actor_label: string;
  action: Permission | "list" | "create" | "update" | "delete";
  outcome: AuditOutcome;
  reason?: string;
  occurred_at: string;
}

export interface CredentialBinding {
  id: string;
  credential_id: string;
  resource_kind: "workspace" | "project" | "agent" | "repo" | "mcp_server";
  resource_id: string;
  resource_label: string;
  created_at: string;
}

export const ALL_PERMISSIONS: Permission[] = [
  "attach",
  "read_redacted",
  "reveal",
  "rotate",
  "revoke",
];

export const PERMISSION_LABEL: Record<Permission, string> = {
  attach: "Attach",
  read_redacted: "Read (redacted)",
  reveal: "Reveal plaintext",
  rotate: "Rotate",
  revoke: "Revoke",
};

export const CREDENTIAL_TYPE_LABEL: Record<CredentialType, string> = {
  repo_deploy_key: "Repo deploy key",
  mcp_bearer: "MCP bearer token",
  api_key: "API key",
  gcp_credentials: "GCP credentials",
  webhook_secret: "Webhook secret",
  sso_cert: "SSO certificate",
  oauth_token: "OAuth token",
  object_storage_key: "Object storage key",
};

// STUB: deterministic mock data used until JEH-1196 backend API ships.
// Swap callers to TanStack Query against /api/credentials once the API exists.

import type {
  AuditEntry,
  Credential,
  CredentialBinding,
  CredentialPolicy,
} from "./types";

const WS = "11bd8321-b6ac-4bee-ae41-6659a5064608";

export const MOCK_CREDENTIALS: Credential[] = [
  {
    id: "cred-001",
    workspace_id: WS,
    type: "repo_deploy_key",
    name: "firtal-cerebro deploy key",
    description: "Read-only key used by Sliplane to clone main",
    status: "active",
    redacted_value: "ssh-ed25519 AAAA***...***xyz",
    created_at: "2026-01-12T09:00:00Z",
    updated_at: "2026-04-02T14:11:00Z",
    last_rotated_at: "2026-04-02T14:11:00Z",
    expires_at: "2026-10-02T14:11:00Z",
  },
  {
    id: "cred-002",
    workspace_id: WS,
    type: "mcp_bearer",
    name: "multica MCP server",
    status: "active",
    redacted_value: "mcp_***...***qK",
    created_at: "2026-02-20T08:15:00Z",
    updated_at: "2026-02-20T08:15:00Z",
    expires_at: "2026-05-22T08:15:00Z",
  },
  {
    id: "cred-003",
    workspace_id: WS,
    type: "api_key",
    name: "FDR API key (prod)",
    description: "Firtal Data Registry — used by dataform jobs",
    status: "active",
    redacted_value: "fdr_***...***A2",
    created_at: "2026-03-01T10:00:00Z",
    updated_at: "2026-03-01T10:00:00Z",
    last_rotated_at: "2026-03-01T10:00:00Z",
  },
  {
    id: "cred-004",
    workspace_id: WS,
    type: "gcp_credentials",
    name: "BigQuery service account",
    status: "active",
    redacted_value: "{...}",
    created_at: "2025-11-04T07:00:00Z",
    updated_at: "2025-11-04T07:00:00Z",
  },
  {
    id: "cred-005",
    workspace_id: WS,
    type: "webhook_secret",
    name: "GitHub deploy webhook",
    status: "expired",
    redacted_value: "whsec_***...***99",
    created_at: "2025-08-01T12:00:00Z",
    updated_at: "2026-04-30T00:00:00Z",
    expires_at: "2026-04-30T00:00:00Z",
  },
  {
    id: "cred-006",
    workspace_id: WS,
    type: "oauth_token",
    name: "Slack OAuth (notifications)",
    status: "revoked",
    redacted_value: "xoxb-***...***",
    created_at: "2026-01-04T14:00:00Z",
    updated_at: "2026-04-20T09:30:00Z",
  },
];

export const MOCK_POLICIES: CredentialPolicy[] = [
  {
    credential_id: "cred-001",
    subject_kind: "member",
    subject_id: "jesperhvejsel@gmail.com",
    subject_label: "Jesper Hvejsel",
    permissions: ["attach", "read_redacted", "reveal", "rotate", "revoke"],
  },
  {
    credential_id: "cred-001",
    subject_kind: "agent",
    subject_id: "fa932ce8-d061-43e9-af23-0731ef5b3bbd",
    subject_label: "Rasp (CTO)",
    permissions: ["attach", "read_redacted"],
  },
  {
    credential_id: "cred-002",
    subject_kind: "agent",
    subject_id: "43501ed6-0b4d-489b-b05e-e5d07e665d91",
    subject_label: "Sara",
    permissions: ["attach", "read_redacted", "rotate"],
  },
  {
    credential_id: "cred-003",
    subject_kind: "group",
    subject_id: "grp-dataform",
    subject_label: "dataform-jobs",
    permissions: ["attach", "read_redacted"],
  },
];

export const MOCK_AUDIT: AuditEntry[] = [
  {
    id: "aud-001",
    credential_id: "cred-001",
    actor_kind: "agent",
    actor_id: "fa932ce8-d061-43e9-af23-0731ef5b3bbd",
    actor_label: "Rasp (CTO)",
    action: "attach",
    outcome: "allow",
    occurred_at: "2026-05-13T17:42:00Z",
  },
  {
    id: "aud-002",
    credential_id: "cred-001",
    actor_kind: "member",
    actor_id: "jesperhvejsel@gmail.com",
    actor_label: "Jesper Hvejsel",
    action: "reveal",
    outcome: "allow",
    occurred_at: "2026-04-02T14:11:00Z",
    reason: "rotation",
  },
  {
    id: "aud-003",
    credential_id: "cred-002",
    actor_kind: "agent",
    actor_id: "unknown",
    actor_label: "unknown agent",
    action: "reveal",
    outcome: "deny",
    occurred_at: "2026-05-10T08:21:00Z",
    reason: "missing reveal permission",
  },
  {
    id: "aud-004",
    credential_id: "cred-005",
    actor_kind: "member",
    actor_id: "jesperhvejsel@gmail.com",
    actor_label: "Jesper Hvejsel",
    action: "revoke",
    outcome: "allow",
    occurred_at: "2026-04-30T00:00:00Z",
    reason: "expired",
  },
];

export const MOCK_BINDINGS: CredentialBinding[] = [
  {
    id: "bind-001",
    credential_id: "cred-001",
    resource_kind: "repo",
    resource_id: "firtal-group/firtal-cerebro",
    resource_label: "firtal-cerebro",
    created_at: "2026-01-12T09:00:00Z",
  },
  {
    id: "bind-002",
    credential_id: "cred-002",
    resource_kind: "mcp_server",
    resource_id: "multica",
    resource_label: "multica MCP",
    created_at: "2026-02-20T08:15:00Z",
  },
];

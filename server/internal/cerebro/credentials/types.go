// Package credentials implements the cerebro credential registry — the
// central store for every underlying secret Multica needs to govern (repo
// deploy keys, MCP bearer tokens, generic API keys, GCP service-account
// credentials, webhook signing secrets, SSO/SAML certs, OAuth tokens,
// object-storage credentials). The registry is workspace-scoped, the
// plaintext is encrypted at rest with AES-256-GCM, and access is split
// into "metadata" (cheap, default GET) vs "reveal" (separate endpoint, every
// call is written to cerebro_credential_audit).
//
// Backend fundament for JEH-1196. Policy enforcement, rotation scheduling,
// and the management UI live in sibling sub-issues (JEH-1197, JEH-1198,
// JEH-1199 ...).
package credentials

import (
	"encoding/json"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Type is the discriminator stored in cerebro_credential.type. Adding a
// new value here requires bumping the CHECK constraint in the migration.
type Type string

const (
	TypeRepoDeployKey    Type = "repo_deploy_key"
	TypeMCPBearer        Type = "mcp_bearer"
	TypeAPIKey           Type = "api_key"
	TypeGCPCredentials   Type = "gcp_credentials"
	TypeWebhookSecret    Type = "webhook_secret"
	TypeSSOCert          Type = "sso_cert"
	TypeOAuthToken       Type = "oauth_token"
	TypeObjectStorageKey Type = "object_storage_key"
)

// validTypes is the in-Go mirror of the migration CHECK constraint. We
// validate at the boundary so the database error becomes a clean 400
// instead of a 500.
var validTypes = map[Type]struct{}{
	TypeRepoDeployKey:    {},
	TypeMCPBearer:        {},
	TypeAPIKey:           {},
	TypeGCPCredentials:   {},
	TypeWebhookSecret:    {},
	TypeSSOCert:          {},
	TypeOAuthToken:       {},
	TypeObjectStorageKey: {},
}

func (t Type) Valid() bool {
	_, ok := validTypes[t]
	return ok
}

// Action is the audit-log discriminator (cerebro_credential_audit.action).
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionReveal Action = "reveal"
	ActionRotate Action = "rotate"
	ActionBind   Action = "bind"
	ActionUnbind Action = "unbind"
	// ActionRead is recorded only on a policy-denied read attempt.
	// Allowed reads are NOT written to keep the audit volume bounded.
	ActionRead Action = "read"
)

// Result is the cerebro_credential_audit.result column added by
// migration 9025. Every row carries a result; the column defaults to
// 'allow' so the existing reveal/rotate/etc rows backfill cleanly.
const (
	AuditResultAllow = "allow"
	AuditResultDeny  = "deny"
)

// CredentialResponse is the redacted JSON shape returned by list/get. It
// MUST NOT contain plaintext value — only the masked hint and metadata.
type CredentialResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	Type          string          `json:"type"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	ValueHint     string          `json:"value_hint"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	ExpiresAt     *string         `json:"expires_at"`
	LastRotatedAt *string         `json:"last_rotated_at"`
}

func credentialResponseFromModel(c cerebrodb.CerebroCredential) CredentialResponse {
	meta := json.RawMessage(c.Metadata)
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return CredentialResponse{
		ID:            util.UUIDToString(c.ID),
		WorkspaceID:   util.UUIDToString(c.WorkspaceID),
		Type:          c.Type,
		Name:          c.Name,
		Description:   c.Description,
		ValueHint:     c.ValueHint,
		Metadata:      meta,
		CreatedByType: c.CreatedByType,
		CreatedByID:   util.UUIDToString(c.CreatedByID),
		CreatedAt:     util.TimestampToString(c.CreatedAt),
		UpdatedAt:     util.TimestampToString(c.UpdatedAt),
		ExpiresAt:     util.TimestampToPtr(c.ExpiresAt),
		LastRotatedAt: util.TimestampToPtr(c.LastRotatedAt),
	}
}

// RevealResponse is the only shape that carries plaintext value. Returned
// exclusively from POST /credentials/:id/reveal, after the call has been
// recorded in cerebro_credential_audit.
type RevealResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// BindingResponse is the JSON shape for cerebro_credential_binding rows.
type BindingResponse struct {
	ID            string `json:"id"`
	CredentialID  string `json:"credential_id"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	CreatedByType string `json:"created_by_type"`
	CreatedByID   string `json:"created_by_id"`
	CreatedAt     string `json:"created_at"`
}

func bindingResponseFromModel(b cerebrodb.CerebroCredentialBinding) BindingResponse {
	return BindingResponse{
		ID:            util.UUIDToString(b.ID),
		CredentialID:  util.UUIDToString(b.CredentialID),
		ResourceType:  b.ResourceType,
		ResourceID:    util.UUIDToString(b.ResourceID),
		CreatedByType: b.CreatedByType,
		CreatedByID:   util.UUIDToString(b.CreatedByID),
		CreatedAt:     util.TimestampToString(b.CreatedAt),
	}
}

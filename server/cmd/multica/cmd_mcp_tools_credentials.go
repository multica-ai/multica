package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-credentials): credential governance MCP tools (JEH-1199 + JEH-1217 live-wire for credential_list + credential_audit_log).

import (
	"context"
	"fmt"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerCredentialTools wires the credential governance MCP tools to the
// stdio server.
//
// Status (JEH-1217):
//   - `credential_list` and `credential_audit_log` are LIVE — they call
//     the registry REST API mounted at
//     `/api/workspaces/{id}/credentials` (JEH-1196). Values are always
//     redacted; the separate `reveal` flow is gated by JEH-1197's
//     policy chain and is not exposed as an MCP tool here.
//   - `credential_policy_get` / `credential_policy_set` STAY as stubs.
//     Per JEH-1197 the credential policy checker reads from Persona
//     (`firtal-persona`) via `CheckResource`, not from a Multica-local
//     table. The Persona SDK does not expose subject-grant CRUD, so
//     "set a credential policy" from Multica is an open architectural
//     question — see the comment on `credential_policy_set` below.
//
// The two policy tools keep their `_stub: true` marker so callers can
// tell they're not looking at persisted data.
func registerCredentialTools(srv *mcp.Server, client *cli.APIClient, workspaceID string) {
	// -----------------------------------------------------------------------
	// credential_list
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_list",
		Description: `List credentials registered in the current workspace.
Returns id, type, name, status, redacted value, created_at, updated_at,
last_rotated_at, and expires_at. Values are ALWAYS redacted — use the
separate reveal flow (gated by policy) if you need plaintext.

Live against /api/workspaces/{id}/credentials (JEH-1196). Falls back to
deterministic mock data if the MCP process has no workspace context.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":   map[string]any{"type": "string", "description": "Filter by credential type (repo_deploy_key, mcp_bearer, api_key, gcp_credentials, webhook_secret, sso_cert, oauth_token, object_storage_key)"},
				"status": map[string]any{"type": "string", "description": "Filter by status (active, expired, revoked)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		typeFilter := optString(args, "type")
		statusFilter := optString(args, "status")
		if workspaceID == "" || client == nil {
			out := filterMockCredentials(typeFilter, statusFilter)
			return jsonText(map[string]any{"credentials": out, "_stub": true})
		}
		path := fmt.Sprintf("/api/workspaces/%s/credentials", url.PathEscape(workspaceID))
		var resp struct {
			Credentials []map[string]any `json:"credentials"`
		}
		if err := client.GetJSON(ctx, path, &resp); err != nil {
			return mcp.ErrorResult("credential_list: " + err.Error()), nil
		}
		out := make([]map[string]any, 0, len(resp.Credentials))
		for _, c := range resp.Credentials {
			if typeFilter != "" && c["type"] != typeFilter {
				continue
			}
			if statusFilter != "" && c["status"] != statusFilter {
				continue
			}
			out = append(out, c)
		}
		return jsonText(map[string]any{"credentials": out})
	})

	// -----------------------------------------------------------------------
	// credential_policy_get
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_policy_get",
		Description: `Return the policy rows for a credential — each row maps a subject
(member, agent, or group) to the permissions they hold on that credential
(attach, read_redacted, reveal, rotate, revoke).

STUB: credential policies are evaluated by Persona at check-time, and
Persona does not expose a "list grants for resource" API. Returns
deterministic mock data so MCP callers can prototype against the
contract. See JEH-1217 for the architectural question.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"credential_id"},
			"properties": map[string]any{
				"credential_id": map[string]any{"type": "string", "description": "Credential ID"},
			},
		},
	}, func(_ context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "credential_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		out := []mockPolicy{}
		for _, p := range mockPolicies {
			if p.CredentialID == id {
				out = append(out, p)
			}
		}
		return jsonText(map[string]any{
			"credential_id": id,
			"policies":      out,
			"_stub":         true,
		})
	})

	// -----------------------------------------------------------------------
	// credential_policy_set
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_policy_set",
		Description: `Set or update the permissions a subject holds on a credential.
Pass the full list of permissions you want the subject to hold — the
backend treats this as a replace, not a merge. Permissions: attach,
read_redacted, reveal, rotate, revoke.

STUB: returns a confirmation echo without persisting anything. Per
JEH-1197 the credential policy authority is Persona, but Persona's SDK
does not expose subject-grant CRUD. Architectural question tracked
under JEH-1217 — either Persona grows a grant API, or credentials
get re-pointed at the local cerebro_workspace_grants table.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"credential_id", "subject_kind", "subject_id", "permissions"},
			"properties": map[string]any{
				"credential_id": map[string]any{"type": "string", "description": "Credential ID"},
				"subject_kind":  map[string]any{"type": "string", "description": "member | agent | group"},
				"subject_id":    map[string]any{"type": "string", "description": "Subject ID"},
				"permissions": map[string]any{
					"type":        "array",
					"description": "Permissions to grant (replace semantics). Use empty array to revoke all.",
					"items":       map[string]any{"type": "string"},
				},
			},
		},
	}, func(_ context.Context, args map[string]any) (mcp.CallToolResult, error) {
		credID, err := requireString(args, "credential_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		subjKind, err := requireString(args, "subject_kind")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		subjID, err := requireString(args, "subject_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		raw, ok := args["permissions"]
		if !ok {
			return mcp.ErrorResult("missing required parameter: permissions"), nil
		}
		list, ok := raw.([]any)
		if !ok {
			return mcp.ErrorResult("permissions must be an array of strings"), nil
		}
		perms := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return mcp.ErrorResult("permissions must be an array of strings"), nil
			}
			perms = append(perms, s)
		}
		return jsonText(map[string]any{
			"ok":            true,
			"credential_id": credID,
			"subject_kind":  subjKind,
			"subject_id":    subjID,
			"permissions":   perms,
			"_stub":         true,
			"_note":         "Not persisted. Set credential policies in the Persona admin UI until JEH-1217's architectural question is resolved.",
		})
	})

	// -----------------------------------------------------------------------
	// credential_audit_log
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_audit_log",
		Description: `Return the audit log for a credential: who did what, when, and
whether the action was allowed or denied. Filter optionally by actor_id
or action.

Live against /api/workspaces/{id}/credentials/{credId}/audit (JEH-1196 +
JEH-1197). Falls back to deterministic mock data if the MCP process has
no workspace context.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"credential_id"},
			"properties": map[string]any{
				"credential_id": map[string]any{"type": "string", "description": "Credential ID"},
				"actor_id":      map[string]any{"type": "string", "description": "Filter by actor"},
				"action":        map[string]any{"type": "string", "description": "Filter by action (attach, read_redacted, reveal, rotate, revoke)"},
				"limit":         map[string]any{"type": "integer", "description": "Max audit rows (default 100, max 1000)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		credID, err := requireString(args, "credential_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		actorFilter := optString(args, "actor_id")
		actionFilter := optString(args, "action")
		if workspaceID == "" || client == nil {
			out := []mockAuditEntry{}
			for _, e := range mockAuditEntries {
				if e.CredentialID != credID {
					continue
				}
				if actorFilter != "" && e.ActorID != actorFilter {
					continue
				}
				if actionFilter != "" && e.Action != actionFilter {
					continue
				}
				out = append(out, e)
			}
			return jsonText(map[string]any{
				"credential_id": credID,
				"entries":       out,
				"_stub":         true,
			})
		}
		path := fmt.Sprintf(
			"/api/workspaces/%s/credentials/%s/audit",
			url.PathEscape(workspaceID),
			url.PathEscape(credID),
		)
		if limit := optInt(args, "limit", 0); limit > 0 {
			path += fmt.Sprintf("?limit=%d", limit)
		}
		var resp struct {
			Audit []map[string]any `json:"audit"`
		}
		if err := client.GetJSON(ctx, path, &resp); err != nil {
			return mcp.ErrorResult("credential_audit_log: " + err.Error()), nil
		}
		entries := make([]map[string]any, 0, len(resp.Audit))
		for _, e := range resp.Audit {
			if actorFilter != "" && e["actor_id"] != actorFilter {
				continue
			}
			if actionFilter != "" && e["action"] != actionFilter {
				continue
			}
			entries = append(entries, e)
		}
		return jsonText(map[string]any{
			"credential_id": credID,
			"entries":       entries,
		})
	})
}

// -----------------------------------------------------------------------------
// Mock fixtures — kept as a deterministic fallback for callers that run the
// MCP server without a configured workspace (e.g. early init, tests).
// -----------------------------------------------------------------------------

type mockCredential struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Status        string `json:"status"`
	RedactedValue string `json:"redacted_value"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	LastRotatedAt string `json:"last_rotated_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type mockPolicy struct {
	CredentialID string   `json:"credential_id"`
	SubjectKind  string   `json:"subject_kind"`
	SubjectID    string   `json:"subject_id"`
	SubjectLabel string   `json:"subject_label"`
	Permissions  []string `json:"permissions"`
}

type mockAuditEntry struct {
	ID           string `json:"id"`
	CredentialID string `json:"credential_id"`
	ActorKind    string `json:"actor_kind"`
	ActorID      string `json:"actor_id"`
	ActorLabel   string `json:"actor_label"`
	Action       string `json:"action"`
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason,omitempty"`
	OccurredAt   string `json:"occurred_at"`
}

const mockWorkspaceID = "11bd8321-b6ac-4bee-ae41-6659a5064608"

var mockCredentials = []mockCredential{
	{
		ID: "cred-001", WorkspaceID: mockWorkspaceID, Type: "repo_deploy_key",
		Name: "firtal-cerebro deploy key", Description: "Read-only key used by Sliplane to clone main",
		Status: "active", RedactedValue: "ssh-ed25519 AAAA***...***xyz",
		CreatedAt: "2026-01-12T09:00:00Z", UpdatedAt: "2026-04-02T14:11:00Z",
		LastRotatedAt: "2026-04-02T14:11:00Z", ExpiresAt: "2026-10-02T14:11:00Z",
	},
	{
		ID: "cred-002", WorkspaceID: mockWorkspaceID, Type: "mcp_bearer",
		Name: "multica MCP server",
		Status: "active", RedactedValue: "mcp_***...***qK",
		CreatedAt: "2026-02-20T08:15:00Z", UpdatedAt: "2026-02-20T08:15:00Z",
		ExpiresAt: "2026-05-22T08:15:00Z",
	},
	{
		ID: "cred-003", WorkspaceID: mockWorkspaceID, Type: "api_key",
		Name: "FDR API key (prod)", Description: "Firtal Data Registry — used by dataform jobs",
		Status: "active", RedactedValue: "fdr_***...***A2",
		CreatedAt: "2026-03-01T10:00:00Z", UpdatedAt: "2026-03-01T10:00:00Z",
		LastRotatedAt: "2026-03-01T10:00:00Z",
	},
	{
		ID: "cred-005", WorkspaceID: mockWorkspaceID, Type: "webhook_secret",
		Name: "GitHub deploy webhook",
		Status: "expired", RedactedValue: "whsec_***...***99",
		CreatedAt: "2025-08-01T12:00:00Z", UpdatedAt: "2026-04-30T00:00:00Z",
		ExpiresAt: "2026-04-30T00:00:00Z",
	},
	{
		ID: "cred-006", WorkspaceID: mockWorkspaceID, Type: "oauth_token",
		Name: "Slack OAuth (notifications)",
		Status: "revoked", RedactedValue: "xoxb-***...***",
		CreatedAt: "2026-01-04T14:00:00Z", UpdatedAt: "2026-04-20T09:30:00Z",
	},
}

var mockPolicies = []mockPolicy{
	{
		CredentialID: "cred-001", SubjectKind: "member",
		SubjectID: "jesperhvejsel@gmail.com", SubjectLabel: "Jesper Hvejsel",
		Permissions: []string{"attach", "read_redacted", "reveal", "rotate", "revoke"},
	},
	{
		CredentialID: "cred-001", SubjectKind: "agent",
		SubjectID: "fa932ce8-d061-43e9-af23-0731ef5b3bbd", SubjectLabel: "Rasp (CTO)",
		Permissions: []string{"attach", "read_redacted"},
	},
	{
		CredentialID: "cred-002", SubjectKind: "agent",
		SubjectID: "43501ed6-0b4d-489b-b05e-e5d07e665d91", SubjectLabel: "Sara",
		Permissions: []string{"attach", "read_redacted", "rotate"},
	},
	{
		CredentialID: "cred-003", SubjectKind: "group",
		SubjectID: "grp-dataform", SubjectLabel: "dataform-jobs",
		Permissions: []string{"attach", "read_redacted"},
	},
}

var mockAuditEntries = []mockAuditEntry{
	{
		ID: "aud-001", CredentialID: "cred-001",
		ActorKind: "agent", ActorID: "fa932ce8-d061-43e9-af23-0731ef5b3bbd", ActorLabel: "Rasp (CTO)",
		Action: "attach", Outcome: "allow",
		OccurredAt: "2026-05-13T17:42:00Z",
	},
	{
		ID: "aud-002", CredentialID: "cred-001",
		ActorKind: "member", ActorID: "jesperhvejsel@gmail.com", ActorLabel: "Jesper Hvejsel",
		Action: "reveal", Outcome: "allow", Reason: "rotation",
		OccurredAt: "2026-04-02T14:11:00Z",
	},
	{
		ID: "aud-003", CredentialID: "cred-002",
		ActorKind: "agent", ActorID: "unknown", ActorLabel: "unknown agent",
		Action: "reveal", Outcome: "deny", Reason: "missing reveal permission",
		OccurredAt: "2026-05-10T08:21:00Z",
	},
	{
		ID: "aud-004", CredentialID: "cred-005",
		ActorKind: "member", ActorID: "jesperhvejsel@gmail.com", ActorLabel: "Jesper Hvejsel",
		Action: "revoke", Outcome: "allow", Reason: "expired",
		OccurredAt: "2026-04-30T00:00:00Z",
	},
}

func filterMockCredentials(typeFilter, statusFilter string) []mockCredential {
	out := make([]mockCredential, 0, len(mockCredentials))
	for _, c := range mockCredentials {
		if typeFilter != "" && c.Type != typeFilter {
			continue
		}
		if statusFilter != "" && c.Status != statusFilter {
			continue
		}
		out = append(out, c)
	}
	return out
}

package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-credentials): credential governance MCP tools (JEH-1199)

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// CEREBRO-PATCH(mcp-credentials-tools-jeh1217): refresh stub deps comments.
// registerCredentialTools wires the credential governance MCP tools to the
// stdio server.
//
// Status (JEH-1217):
//   - The credential registry REST API (JEH-1196) HAS shipped at
//     `/api/workspaces/{id}/credentials` — `credential_list`,
//     `credential_audit_log` could be wired to it now. Tracked as a
//     follow-up to JEH-1199 (UI wiring + this MCP wiring should land
//     together so admins see consistent state from both surfaces).
//   - `credential_policy_get` / `credential_policy_set` cannot be wired
//     persistently until JEH-1179 (Persona grants admin API) ships, since
//     per JEH-1197 the policy checker reads Persona grants. Until then
//     these tools return a `_stub` echo so callers can prototype against
//     the contract.
//
// Each handler keeps the `_stub: true` marker so MCP callers can tell
// they're not looking at live data.
func registerCredentialTools(srv *mcp.Server, _ *cli.APIClient) {
	// -----------------------------------------------------------------------
	// credential_list
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_list",
		Description: `List credentials registered in the current workspace.
Returns id, type, name, status, redacted value, created_at, updated_at,
last_rotated_at, and expires_at. Values are ALWAYS redacted — use the
separate reveal flow (gated by policy) if you need plaintext.

STUB: returns deterministic mock data. Live wiring to JEH-1196's /api/workspaces/{id}/credentials is tracked as JEH-1199 follow-up.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":   map[string]any{"type": "string", "description": "Filter by credential type (repo_deploy_key, mcp_bearer, api_key, gcp_credentials, webhook_secret, sso_cert, oauth_token, object_storage_key)"},
				"status": map[string]any{"type": "string", "description": "Filter by status (active, expired, revoked)"},
			},
		},
	}, func(_ context.Context, args map[string]any) (mcp.CallToolResult, error) {
		out := filterMockCredentials(optString(args, "type"), optString(args, "status"))
		return jsonText(map[string]any{"credentials": out, "_stub": true})
	})

	// -----------------------------------------------------------------------
	// credential_policy_get
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "credential_policy_get",
		Description: `Return the policy rows for a credential — each row maps a subject
(member, agent, or group) to the permissions they hold on that credential
(attach, read_redacted, reveal, rotate, revoke).

STUB: returns deterministic mock data. Live wiring to JEH-1196's /api/workspaces/{id}/credentials is tracked as JEH-1199 follow-up.`,
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

STUB: returns a confirmation echo without persisting anything. Real
persistence depends on JEH-1179 (Persona grants admin API), since
per JEH-1197 credential policy = persona grant against the credential.`,
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
			"_note":         "Stubbed write — no persistence until JEH-1196 lands.",
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

STUB: returns deterministic mock data. Live wiring to JEH-1196's /api/workspaces/{id}/credentials is tracked as JEH-1199 follow-up.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"credential_id"},
			"properties": map[string]any{
				"credential_id": map[string]any{"type": "string", "description": "Credential ID"},
				"actor_id":      map[string]any{"type": "string", "description": "Filter by actor"},
				"action":        map[string]any{"type": "string", "description": "Filter by action (attach, read_redacted, reveal, rotate, revoke)"},
			},
		},
	}, func(_ context.Context, args map[string]any) (mcp.CallToolResult, error) {
		credID, err := requireString(args, "credential_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		actorFilter := optString(args, "actor_id")
		actionFilter := optString(args, "action")
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
	})
}

// -----------------------------------------------------------------------------
// Mock fixtures — match the @multica/cerebro-credentials TS types so the UI
// and MCP layer agree on the contract until JEH-1196 lands.
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

func ts(year int, month time.Month, day, hour, minute int) string {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC).Format(time.RFC3339)
}

var mockCredentials = []mockCredential{
	{
		ID: "cred-001", WorkspaceID: mockWorkspaceID, Type: "repo_deploy_key",
		Name: "firtal-cerebro deploy key", Description: "Read-only key used by Sliplane to clone main",
		Status: "active", RedactedValue: "ssh-ed25519 AAAA***...***xyz",
		CreatedAt: ts(2026, time.January, 12, 9, 0), UpdatedAt: ts(2026, time.April, 2, 14, 11),
		LastRotatedAt: ts(2026, time.April, 2, 14, 11), ExpiresAt: ts(2026, time.October, 2, 14, 11),
	},
	{
		ID: "cred-002", WorkspaceID: mockWorkspaceID, Type: "mcp_bearer",
		Name: "multica MCP server",
		Status: "active", RedactedValue: "mcp_***...***qK",
		CreatedAt: ts(2026, time.February, 20, 8, 15), UpdatedAt: ts(2026, time.February, 20, 8, 15),
		ExpiresAt: ts(2026, time.May, 22, 8, 15),
	},
	{
		ID: "cred-003", WorkspaceID: mockWorkspaceID, Type: "api_key",
		Name: "FDR API key (prod)", Description: "Firtal Data Registry — used by dataform jobs",
		Status: "active", RedactedValue: "fdr_***...***A2",
		CreatedAt: ts(2026, time.March, 1, 10, 0), UpdatedAt: ts(2026, time.March, 1, 10, 0),
		LastRotatedAt: ts(2026, time.March, 1, 10, 0),
	},
	{
		ID: "cred-005", WorkspaceID: mockWorkspaceID, Type: "webhook_secret",
		Name: "GitHub deploy webhook",
		Status: "expired", RedactedValue: "whsec_***...***99",
		CreatedAt: ts(2025, time.August, 1, 12, 0), UpdatedAt: ts(2026, time.April, 30, 0, 0),
		ExpiresAt: ts(2026, time.April, 30, 0, 0),
	},
	{
		ID: "cred-006", WorkspaceID: mockWorkspaceID, Type: "oauth_token",
		Name: "Slack OAuth (notifications)",
		Status: "revoked", RedactedValue: "xoxb-***...***",
		CreatedAt: ts(2026, time.January, 4, 14, 0), UpdatedAt: ts(2026, time.April, 20, 9, 30),
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
		OccurredAt: ts(2026, time.May, 13, 17, 42),
	},
	{
		ID: "aud-002", CredentialID: "cred-001",
		ActorKind: "member", ActorID: "jesperhvejsel@gmail.com", ActorLabel: "Jesper Hvejsel",
		Action: "reveal", Outcome: "allow", Reason: "rotation",
		OccurredAt: ts(2026, time.April, 2, 14, 11),
	},
	{
		ID: "aud-003", CredentialID: "cred-002",
		ActorKind: "agent", ActorID: "unknown", ActorLabel: "unknown agent",
		Action: "reveal", Outcome: "deny", Reason: "missing reveal permission",
		OccurredAt: ts(2026, time.May, 10, 8, 21),
	},
	{
		ID: "aud-004", CredentialID: "cred-005",
		ActorKind: "member", ActorID: "jesperhvejsel@gmail.com", ActorLabel: "Jesper Hvejsel",
		Action: "revoke", Outcome: "allow", Reason: "expired",
		OccurredAt: ts(2026, time.April, 30, 0, 0),
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

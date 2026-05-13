package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-grants): new cerebro file for the
// Persona grant control plane MCP tools (JEH-1181). Lives next to the
// existing cmd_mcp_tools_*.go files so the feature is additive — the only
// edit to cmd_mcp_tools.go is the registerGrantTools(...) call site.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cerebro/persona"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// grantsBackendFactory builds the backend used by the grant tools. JEH-1181
// ships ahead of JEH-1179, so today this always returns the in-memory mock.
// When JEH-1179 lands, swap the body for an HTTP-backed implementation that
// talks to /api/workspaces/{id}/grants. The MCP tool signatures stay
// identical.
var grantsBackendFactory = func(workspaceID string) persona.Backend {
	return persona.NewMockBackend(workspaceID)
}

// mcpProcessSessionID identifies this `multica mcp serve` process in audit
// logs. Generated once at registration time so a sequence of tool calls
// from one Claude session is groupable in the log.
var mcpProcessSessionID = uuid.NewString()

// registerGrantTools wires the Persona grant MCP tools onto srv. The
// `actor`/`actorType` are captured from the authenticated multica user at
// startup so audit entries record "actor X via MCP-session Y …" without an
// extra /api/me roundtrip per call. The caller is expected to pass empty
// strings if it could not resolve the identity — in that case the tools
// still work but audit entries are anonymous.
func registerGrantTools(srv *mcp.Server, workspaceID, actor, actorType string) {
	backend := grantsBackendFactory(workspaceID)
	ac := func() persona.AuditContext {
		return persona.AuditContext{
			Actor:     actor,
			ActorType: actorType,
			Surface:   "mcp",
			SessionID: mcpProcessSessionID,
		}
	}

	// -----------------------------------------------------------------------
	// list_grants
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "list_grants",
		Description: `List Persona grants in the current workspace. A grant authorises a subject (member/agent/group/role/workspace_default/anonymous) to use a capability against a resource pattern, optionally bounded by classification ceiling, time window, and approval requirements.

Use this BEFORE proposing a new grant — duplicates and overlapping rules cause confusion when an admin reads the policy list.

STUB STATUS: until JEH-1179 lands, results are served from a local in-memory mock per ` + "`multica mcp serve`" + ` process. The tool shape matches the proposed API and will not change when the real backend is wired.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject_type":  map[string]any{"type": "string", "description": "Filter by subject type: member, agent, group, role, workspace_default, anonymous"},
				"subject_id":    map[string]any{"type": "string", "description": "Filter by exact subject id"},
				"capability":    map[string]any{"type": "string", "description": "Filter by exact capability (e.g. issue.read)"},
				"resource_like": map[string]any{"type": "string", "description": "Filter by substring match on resource_pattern"},
				"effect":        map[string]any{"type": "string", "description": "Filter by effect: allow or deny"},
				"status":        map[string]any{"type": "string", "description": "Filter by status: active or revoked"},
				"limit":         map[string]any{"type": "integer", "description": "Max results (default 50)"},
				"offset":        map[string]any{"type": "integer", "description": "Offset for pagination"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		f := persona.ListFilter{
			SubjectType:  optString(args, "subject_type"),
			SubjectID:    optString(args, "subject_id"),
			Capability:   optString(args, "capability"),
			ResourceLike: optString(args, "resource_like"),
			Effect:       optString(args, "effect"),
			Status:       optString(args, "status"),
			Limit:        optInt(args, "limit", 50),
			Offset:       optInt(args, "offset", 0),
		}
		grants, err := backend.List(ctx, f, ac())
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"grants": grants, "count": len(grants)})
	})

	// -----------------------------------------------------------------------
	// get_grant
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "get_grant",
		Description: `Fetch one grant by id with full detail, including validity window, approval requirement, and current status. Use this before suggesting edits so the proposed change is grounded in the current grant state, not a stale list-view snapshot.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"grant_id"},
			"properties": map[string]any{
				"grant_id": map[string]any{"type": "string", "description": "Grant ID (UUID)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "grant_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		g, err := backend.Get(ctx, id, ac())
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(g)
	})

	// -----------------------------------------------------------------------
	// create_grant
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "create_grant",
		Description: `Create a Persona grant.

WHEN TO USE:
- Admin asks "let agent X read issues in workspace W" — express as a single grant.
- Granting an emergency override — set ` + "`ends_at`" + ` so the grant auto-expires.
- Adding a deny rule — set ` + "`effect=deny`" + `; deny wins over conflicting allow grants.

SAFETY GUIDELINES:
- Always prefer narrow ` + "`resource_pattern`" + `s over wildcards.
- For time-bound access, set both ` + "`starts_at`" + ` and ` + "`ends_at`" + `.
- When the action is sensitive (writes, deletes, billing), set ` + "`requires_approval=true`" + ` so Persona blocks until an approver signs off.
- Classification ceiling caps the grant — a grant with ceiling=internal will NOT cover a confidential resource even if the pattern matches.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"subject_type", "resource_pattern", "capability"},
			"properties": map[string]any{
				"subject_type":           map[string]any{"type": "string", "description": "member, agent, group, role, workspace_default, anonymous"},
				"subject_id":             map[string]any{"type": "string", "description": "Subject id (omit only for workspace_default / anonymous)"},
				"resource_pattern":       map[string]any{"type": "string", "description": "Typed resource pattern, e.g. 'issue:*', 'agent:<uuid>', 'project:*'"},
				"capability":             map[string]any{"type": "string", "description": "Canonical capability, e.g. issue.read, issue.write, agent.run"},
				"classification_ceiling": map[string]any{"type": "string", "description": "public | internal | confidential | restricted"},
				"starts_at":              map[string]any{"type": "string", "description": "RFC3339 — when the grant becomes effective (optional)"},
				"ends_at":                map[string]any{"type": "string", "description": "RFC3339 — when the grant expires (optional)"},
				"requires_approval":      map[string]any{"type": "boolean", "description": "If true, Persona blocks execution until an approver signs off"},
				"approval_role":          map[string]any{"type": "string", "description": "Role expected to approve (e.g. workspace_owner)"},
				"effect":                 map[string]any{"type": "string", "description": "allow (default) or deny"},
				"status":                 map[string]any{"type": "string", "description": "active (default) or revoked"},
				"note":                   map[string]any{"type": "string", "description": "Free-text reason — shown in audit and admin UI"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		in, err := parseGrantInput(args)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		g, err := backend.Create(ctx, in, ac())
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(g)
	})

	// -----------------------------------------------------------------------
	// update_grant
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "update_grant",
		Description: `Update an existing grant. Only fields you provide are changed; omit a field to leave it untouched.

To clear an optional field, pass an empty string for string fields. To revoke a grant, prefer ` + "`delete_grant`" + ` over ` + "`status=revoked`" + ` — they have the same effect but ` + "`delete_grant`" + ` reads more clearly in the audit log.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"grant_id"},
			"properties": map[string]any{
				"grant_id":               map[string]any{"type": "string", "description": "Grant id"},
				"subject_type":           map[string]any{"type": "string"},
				"subject_id":             map[string]any{"type": "string"},
				"resource_pattern":       map[string]any{"type": "string"},
				"capability":             map[string]any{"type": "string"},
				"classification_ceiling": map[string]any{"type": "string"},
				"starts_at":              map[string]any{"type": "string", "description": "RFC3339"},
				"ends_at":                map[string]any{"type": "string", "description": "RFC3339"},
				"requires_approval":      map[string]any{"type": "boolean"},
				"approval_role":          map[string]any{"type": "string"},
				"effect":                 map[string]any{"type": "string"},
				"status":                 map[string]any{"type": "string"},
				"note":                   map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "grant_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		in, err := parseGrantInput(args)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		g, err := backend.Update(ctx, id, in, ac())
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(g)
	})

	// -----------------------------------------------------------------------
	// delete_grant
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "delete_grant",
		Description: `Revoke a grant. This is a soft-delete: the grant row stays so the audit log can reference it; ` + "`status`" + ` flips to revoked and Persona stops honouring it.

Use this for routine policy clean-up. For "stop the bleeding now" scenarios, prefer creating a high-priority ` + "`effect=deny`" + ` grant covering the same subject + capability — that takes effect immediately and survives a debate about whether the offending grant should also exist.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"grant_id"},
			"properties": map[string]any{
				"grant_id": map[string]any{"type": "string", "description": "Grant id to revoke"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "grant_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := backend.Delete(ctx, id, ac()); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return mcp.TextResult(fmt.Sprintf("grant %s revoked", id)), nil
	})

	// -----------------------------------------------------------------------
	// evaluate_policy
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "evaluate_policy",
		Description: `Dry-run a Persona permission check: "if actor X did capability Y against resource Z, what would Persona answer and why?"

USE FOR:
- Verifying a proposed grant before creating it.
- Debugging "why is agent X blocked from doing Y?" — the returned ` + "`matched_grants`" + ` lists every grant that influenced the decision, and ` + "`winning_grant`" + ` points at the one that decided it.
- Explaining policy to a human in admin chat.

EVALUATION RULES:
1. Default deny — no matching grants = deny.
2. Any matching deny grant wins, even over allow grants.
3. Classification ceiling on the matching allow grant must cover the resource's classification, else the grant does not apply.
4. Validity window (starts_at / ends_at) and status=active are required.

NOTE: in the mock, group/role membership is NOT expanded — only exact subject_type+subject_id matches the actor. When JEH-1179 lands the real backend will resolve group/role/workspace_default expansion.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"actor_type", "actor_id", "resource", "capability"},
			"properties": map[string]any{
				"actor_type":              map[string]any{"type": "string", "description": "member or agent (the entity performing the action)"},
				"actor_id":                map[string]any{"type": "string", "description": "Actor id (UUID or stable identifier)"},
				"resource":                map[string]any{"type": "string", "description": "Resource string, e.g. 'issue:JEH-1181' or 'agent:<uuid>'"},
				"capability":              map[string]any{"type": "string", "description": "Capability the actor is attempting, e.g. issue.read"},
				"resource_classification": map[string]any{"type": "string", "description": "Classification of the target resource (public | internal | confidential | restricted)"},
				"at":                      map[string]any{"type": "string", "description": "RFC3339 — evaluate as-of this time (defaults to now)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		q := persona.EvaluationQuery{
			ActorType:              optString(args, "actor_type"),
			ActorID:                optString(args, "actor_id"),
			Resource:               optString(args, "resource"),
			Capability:             optString(args, "capability"),
			ResourceClassification: optString(args, "resource_classification"),
		}
		if at := optString(args, "at"); at != "" {
			t, err := time.Parse(time.RFC3339, at)
			if err != nil {
				return mcp.ErrorResult(fmt.Sprintf("invalid 'at' (need RFC3339): %v", err)), nil
			}
			q.At = &t
		}
		res, err := backend.Evaluate(ctx, q, ac())
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(res)
	})

	// -----------------------------------------------------------------------
	// list_grant_audit
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "list_grant_audit",
		Description: `List the audit-log entries for grant mutations and policy evaluations done via this MCP session. Use to answer "who changed this grant and when" or to surface a paper trail for a compliance question.

NOTE: in the stub, the log is in-memory per ` + "`multica mcp serve`" + ` process. When JEH-1179 lands, this reads from the shared audit store.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"grant_id": map[string]any{"type": "string", "description": "Filter to entries about this grant id (optional)"},
				"limit":    map[string]any{"type": "integer", "description": "Max entries (default 50)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		limit := optInt(args, "limit", 50)
		entries, err := backend.ListAudit(ctx, optString(args, "grant_id"), limit)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"entries": entries, "count": len(entries)})
	})
}

// resolveMCPActor calls /api/me once to capture the authenticated user for
// audit-log attribution. Failure is non-fatal: we fall back to empty
// strings and the audit entries are recorded anonymously. We do this once
// at registration time rather than per tool-call so the audit identity is
// stable for the lifetime of the MCP process.
func resolveMCPActor(client *cli.APIClient) (id, kind string) {
	var me struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := client.GetJSON(context.Background(), "/api/me", &me); err != nil {
		return "", ""
	}
	kind = me.Type
	if kind == "" {
		kind = "member"
	}
	return me.ID, kind
}

// parseGrantInput converts MCP tool args into a persona.GrantInput. Only
// keys actually present in args become non-nil pointers — that lets Update
// distinguish "not provided" from "set to empty string".
func parseGrantInput(args map[string]any) (persona.GrantInput, error) {
	var in persona.GrantInput
	if v, ok := stringArg(args, "subject_type"); ok {
		in.SubjectType = &v
	}
	if v, ok := stringArg(args, "subject_id"); ok {
		in.SubjectID = &v
	}
	if v, ok := stringArg(args, "resource_pattern"); ok {
		in.ResourcePattern = &v
	}
	if v, ok := stringArg(args, "capability"); ok {
		in.Capability = &v
	}
	if v, ok := stringArg(args, "classification_ceiling"); ok {
		in.ClassificationCeiling = &v
	}
	if v, ok := stringArg(args, "approval_role"); ok {
		in.ApprovalRole = &v
	}
	if v, ok := stringArg(args, "effect"); ok {
		in.Effect = &v
	}
	if v, ok := stringArg(args, "status"); ok {
		in.Status = &v
	}
	if v, ok := stringArg(args, "note"); ok {
		in.Note = &v
	}
	if raw, ok := args["requires_approval"]; ok {
		b, isBool := raw.(bool)
		if !isBool {
			return in, fmt.Errorf("requires_approval must be boolean")
		}
		in.RequiresApproval = &b
	}
	if raw, ok := stringArg(args, "starts_at"); ok && raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return in, fmt.Errorf("invalid starts_at (need RFC3339): %v", err)
		}
		in.StartsAt = &t
	}
	if raw, ok := stringArg(args, "ends_at"); ok && raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return in, fmt.Errorf("invalid ends_at (need RFC3339): %v", err)
		}
		in.EndsAt = &t
	}
	return in, nil
}

// stringArg returns (value, true) if the key exists and is a string —
// distinguishing "absent" from "present with empty value".
func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, isStr := v.(string)
	if !isStr {
		return "", false
	}
	return s, true
}

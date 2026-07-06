package clitools

// CEREBRO-PATCH(mcp-agent-office-tools): FIR-1775 — expose the Agent Office
// (agent context versioning + governance) flow as MCP tools so agents working
// through MCP can inspect versions, PROPOSE context changes, REVIEW them, diff,
// roll back, and manage ownership — not just via the CLI. Backs onto the same
// REST endpoints the web UI and `multica agent context` CLI use
// (internal/cerebro/agentoffice/handler.go).

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerAgentOfficeTools wires the agent-context governance MCP tools to the
// stdio server. The governance model mirrors skills (FIR-1775): an agent's
// context has an owner and approvers; anyone may PROPOSE a versioned change, but
// only the owner/approvers may APPROVE it, which atomically applies the proposed
// snapshot and records a new version.
//
// NORM FOR AGENTS: to change an agent's instructions/context you do not own, do
// NOT edit the agent directly — open a change request with
// `agent_context_propose` and let the owner review it. This keeps the agent
// harness auditable and reversible (the drift problem this feature exists to
// solve).
func registerAgentOfficeTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// agent_context_versions
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_versions",
		Description: `List the version history of an agent's context (instructions, bound skills,
model, mcp_config, etc.), newest first. Each entry is an append-only snapshot
you can diff against or roll back to.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id"},
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(id)+"/context/versions", &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_change_requests
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_change_requests",
		Description: `List context change requests. Pass agent_id to see one agent's proposals
(any status), or omit it to list all PENDING proposals across the workspace —
the review queue for owners/approvers.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID (optional; omit for the workspace pending queue)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id := optString(args, "agent_id")
		path := "/api/agents/context/change-requests"
		if id != "" {
			path = "/api/agents/" + url.PathEscape(id) + "/context/change-requests"
		}
		var out any
		if err := client.GetJSON(ctx, path, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_propose
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_propose",
		Description: `Propose a versioned change to an agent's context as a change request — the
standard way to change an agent's instructions/skills. The proposal is reviewed
by the context owner/approvers; on approval it is applied atomically and
snapshotted as a new version. Nothing changes until approved.

You may either edit individual fields (instructions, model, thinking_level,
persona_sandbox, skill_ids, mcp_config) which are merged onto the agent's current
context, OR pass a full proposed_snapshot object. proposed_version must be a
semver X.Y.Z strictly greater than the agent's current context_version.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id", "title", "proposed_version"},
			"properties": map[string]any{
				"agent_id":         map[string]any{"type": "string", "description": "Agent ID to propose a change to"},
				"title":            map[string]any{"type": "string", "description": "Short title for the change request"},
				"description":      map[string]any{"type": "string", "description": "What changed and why"},
				"proposed_version": map[string]any{"type": "string", "description": "New semver X.Y.Z, greater than current"},
				"instructions":     map[string]any{"type": "string", "description": "Full replacement instructions (persona/rules text)"},
				"model":            map[string]any{"type": "string", "description": "Model override"},
				"thinking_level":   map[string]any{"type": "string", "description": "Thinking level override"},
				"persona_sandbox":  map[string]any{"type": "string", "description": "Persona sandbox override"},
				"skill_ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Full replacement set of bound skill ids",
				},
				"mcp_config": map[string]any{
					"type":        "object",
					"description": "Full replacement MCP server config object (e.g. {\"mcpServers\": …}); null/omit to leave unchanged",
				},
				"custom_args": map[string]any{
					"description": "Full replacement custom runtime args (JSON)",
				},
				"runtime_config": map[string]any{
					"description": "Full replacement runtime config (JSON)",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		title, err := requireString(args, "title")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		version, err := requireString(args, "proposed_version")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"title": title, "proposed_version": version}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		if v, ok := args["instructions"].(string); ok {
			body["instructions"] = v
		}
		if v, ok := args["model"].(string); ok {
			body["model"] = v
		}
		if v, ok := args["thinking_level"].(string); ok {
			body["thinking_level"] = v
		}
		if v, ok := args["persona_sandbox"].(string); ok {
			body["persona_sandbox"] = v
		}
		if v, ok := args["skill_ids"]; ok {
			body["skill_ids"] = v
		}
		// mcp_config / custom_args / runtime_config are free-form JSON that the
		// API captures as RawMessage; forward them verbatim when present so the
		// MCP/CLI/API surfaces stay symmetric.
		if v, ok := args["mcp_config"]; ok {
			body["mcp_config"] = v
		}
		if v, ok := args["custom_args"]; ok {
			body["custom_args"] = v
		}
		if v, ok := args["runtime_config"]; ok {
			body["runtime_config"] = v
		}
		// Link the proposal to the agent run that produced it when available.
		if taskID := strings.TrimSpace(os.Getenv("MULTICA_TASK_ID")); taskID != "" {
			body["work_session_id"] = taskID
		}
		var out any
		if err := client.PostJSON(ctx, "/api/agents/"+url.PathEscape(id)+"/context/change-requests", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_review
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_review",
		Description: `Approve or reject a pending agent-context change request. Approve applies the
proposed snapshot atomically and snapshots a new version. Only the context
owner/approvers (or a workspace admin) may review.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"change_request_id", "action"},
			"properties": map[string]any{
				"change_request_id": map[string]any{"type": "string", "description": "Change request ID"},
				"action":            map[string]any{"type": "string", "enum": []string{"approve", "reject"}, "description": "approve or reject"},
				"comment":           map[string]any{"type": "string", "description": "Optional review comment"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		crID, err := requireString(args, "change_request_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		action, err := requireString(args, "action")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"action": action}
		if v := optString(args, "comment"); v != "" {
			body["comment"] = v
		}
		var out any
		if err := client.PostJSON(ctx, "/api/agents/context/change-requests/"+url.PathEscape(crID)+"/review", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_diff
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_diff",
		Description: `Show a unified diff between two versions of an agent's context. 'from' is a
required version number; 'to' defaults to the live context when omitted.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id", "from"},
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID"},
				"from":     map[string]any{"type": "string", "description": "Base version (X.Y.Z)"},
				"to":       map[string]any{"type": "string", "description": "Target version (X.Y.Z); omit for live"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		from, err := requireString(args, "from")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		q := url.Values{}
		q.Set("from", from)
		if to := optString(args, "to"); to != "" {
			q.Set("to", to)
		}
		var out any
		if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(id)+"/context/diff?"+q.Encode(), &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_rollback
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_rollback",
		Description: `Propose rolling an agent's context back to a historical version. This does NOT
apply directly: it opens a change request carrying that version's snapshot, which
an approver must review and approve before it merges onto the live agent (history
is append-only — nothing is deleted). Only the context owner/approvers (or a
workspace admin) may propose a rollback.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id", "version"},
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID"},
				"version":  map[string]any{"type": "string", "description": "The historical version to restore (X.Y.Z)"},
				"comment":  map[string]any{"type": "string", "description": "Optional note describing why"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		version, err := requireString(args, "version")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"version": version}
		if v := optString(args, "comment"); v != "" {
			body["comment"] = v
		}
		var out any
		if err := client.PostJSON(ctx, "/api/agents/"+url.PathEscape(id)+"/context/rollback", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	// -----------------------------------------------------------------------
	// agent_context_set_ownership
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "agent_context_set_ownership",
		Description: `Set the context owner and/or approvers for an agent. Owner + approvers are the
users allowed to review and merge context change requests. Omitting a field
leaves it unchanged.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id"},
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID"},
				"owner_id": map[string]any{"type": "string", "description": "New context owner user id (empty string clears)"},
				"approver_ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Full replacement set of approver user ids",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{}
		if v, ok := args["owner_id"].(string); ok {
			body["owner_id"] = v
		}
		if v, ok := args["approver_ids"]; ok {
			body["approver_ids"] = v
		}
		var out any
		if err := client.PutJSON(ctx, "/api/agents/"+url.PathEscape(id)+"/context/ownership", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}

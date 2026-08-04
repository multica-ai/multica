package handler

// CEREBRO-PATCH(daemon-tools-brief): FIR-4500 — server-side resolution of the
// cerebro_tools_brief workspace feature flag at claim time. When a workspace
// turns the flag OFF, the claim response carries ToolsBriefDisabled=true and
// the daemon renders no "Connections & MCP tools" section into the agent's
// runtime brief (see server/internal/daemon/execenv/cerebro_brief_layers.go).
// Like the Workpad brief this reaches ALL local runtime types (Claude, Codex,
// Cursor, Gemini, …), because the brief is assembled from the shared
// TaskContextForEnv with no per-machine setup.
//
// The section is documentation, not capability: a tool is callable through the
// runtime's own tool schemas and the live multica_connection_tools_status
// lookup, never through this prose list. Turning the flag off removes ~7.5k
// tokens from every run without removing a single tool.
//
// The field is named for the OFF state on purpose. Its zero value must mean
// "render as always", so an older server that does not send it cannot make a
// newer daemon silently drop the section.

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// toolsBriefFlagKey mirrors the "cerebro_tools_brief" flag in
// packages/cerebro-feature-flags/registry.ts (the UI source of truth), where it
// defaults to true.
const toolsBriefFlagKey = "cerebro_tools_brief"

// applyToolsBrief sets resp.ToolsBriefDisabled when the issue's workspace has
// the cerebro_tools_brief flag off. Workspace-level only (no per-user override
// — the actor is an agent). Best-effort: cerebroResolveFlag returns the default
// on any lookup failure, and the default here is true, so a flag outage leaves
// the brief exactly as it is today and can never block a real claim.
func (h *Handler) applyToolsBrief(ctx context.Context, resp *AgentTaskResponse, issue db.Issue) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid {
		return
	}
	resp.ToolsBriefDisabled = !h.cerebroResolveFlag(ctx, issue.WorkspaceID, "", toolsBriefFlagKey, true)
}

package handler

// CEREBRO-PATCH(daemon-workpad-brief): FIR-3659 — server-side resolution of the
// cerebro_workpad workspace feature flag at claim time. When a workspace turns
// Workpad "on", the claim response carries WorkpadBriefEnabled=true, and the
// daemon renders the Workpad protocol section into the agent's runtime brief
// (see server/internal/daemon/execenv/cerebro_workpad_brief.go). This reaches
// ALL local runtime types (Claude, Codex, Cursor, Gemini, …) because the brief
// is assembled from the shared TaskContextForEnv, with no per-machine setup.
//
// The brief is guidance only; enforcement of the workpad (blocking an agent's
// move to in_progress without one) is the separate before.issue.status_change
// hook policy backed by the issue_has_workpad eval. Keeping the flag on the
// brief lets the rollout be brief-first, then policy.

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// workpadFlagKey mirrors the "cerebro_workpad" flag in
// packages/cerebro-feature-flags/registry.ts (the UI source of truth).
const workpadFlagKey = "cerebro_workpad"

// applyWorkpadBrief sets resp.WorkpadBriefEnabled when the issue's workspace has
// the cerebro_workpad flag on. It is a workspace-level toggle, so it resolves
// against the workspace default (no per-user override — the actor is an agent).
// Best-effort: cerebroResolveFlag returns false on any lookup failure, so a flag
// outage leaves the brief off exactly as when the feature is disabled, and this
// can never block a real claim.
func (h *Handler) applyWorkpadBrief(ctx context.Context, resp *AgentTaskResponse, issue db.Issue) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid {
		return
	}
	resp.WorkpadBriefEnabled = h.cerebroResolveFlag(ctx, issue.WorkspaceID, "", workpadFlagKey, false)
}

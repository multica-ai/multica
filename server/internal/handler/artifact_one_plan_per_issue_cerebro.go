package handler

// CEREBRO-PATCH(one-plan-per-issue): FIR-3659 — an issue carries at most one
// plan. The Workpad renders that single plan as the issue's checklist, so a
// second plan on the same issue would make "the plan" ambiguous. This guard
// rejects the create of a second plan; the agent must update the existing plan
// instead of forking it.
//
// The upstream CreateArtifact handler only calls rejectSecondIssuePlan in a
// 1-line hook (marked CEREBRO-PATCH there); all logic lives here so the upstream
// touch stays within the ≤5-line patch budget.

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// rejectSecondIssuePlan writes a 409 and returns true when the artifact being
// created is a plan (`kind == "plan"`) and the issue already has one. It is a
// no-op (returns false) for any other kind, when Workpad is off for the
// workspace, or when the issue has no plan yet. A list error is treated as "no
// existing plan" — the create proceeds rather than blocking on a transient read
// failure. Gated by the cerebro_workpad flag (workpadFlagKey, resolved via the
// shared cerebroResolveFlag helper) so the guard stays inert until Workpad is on.
func (h *Handler) rejectSecondIssuePlan(w http.ResponseWriter, r *http.Request, workspaceID, kind string, issueID pgtype.UUID) bool {
	if kind != "plan" {
		return false
	}
	if !h.cerebroResolveFlag(r.Context(), parseUUID(workspaceID), "", workpadFlagKey, false) {
		return false
	}
	existing, err := h.Queries.ListArtifactsByIssue(r.Context(), db.ListArtifactsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return false
	}
	for _, a := range existing {
		if a.Kind == "plan" {
			writeError(w, http.StatusConflict, "issue already has a plan; update the existing plan instead of creating a second one")
			return true
		}
	}
	return false
}

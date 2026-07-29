package handler

// FIR-3901 — the access gates behind the dead-failed-run endpoints.
//
// The endpoints themselves live in server/internal/cerebro/inbox, which cannot
// reach loadIssueForUser / canAccessIssue (both unexported here). Rather than
// grow a second copy of the access rule there — which would drift the moment
// project access or issue privacy changes — the router injects these two
// closures into the cerebro handler. One rule, one home.

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CerebroIssueAccessGate resolves the issue behind /api/issues/{id}/failed-runs
// through the same path as every other issue endpoint: workspace scoping, then
// project access / privacy / channel membership. It writes the error response
// itself and returns ok=false, so the caller only has to return.
//
// The returned UUID is the RESOLVED issue id — callers must query with it, not
// with the raw URL string, so a human identifier ("MUL-123") cannot slip
// through as an unscoped UUID lookup.
func (h *Handler) CerebroIssueAccessGate() func(http.ResponseWriter, *http.Request, string) (pgtype.UUID, bool) {
	return func(w http.ResponseWriter, r *http.Request, issueID string) (pgtype.UUID, bool) {
		issue, ok := h.loadIssueForUser(w, r, issueID)
		if !ok {
			return pgtype.UUID{}, false
		}
		return issue.ID, true
	}
}

// CerebroVisibleIssuesFilter narrows a set of issue ids to the ones this
// request may actually see. Used by the workspace-wide inbox feed, where the
// rows are already workspace-scoped but may still include a private issue or
// one in a project the member is not on.
//
// Returns the allowed ids as a set keyed by the issue's string UUID. An empty
// set means nothing is visible — the filter fails closed on every error path.
func (h *Handler) CerebroVisibleIssuesFilter() func(*http.Request, []pgtype.UUID) map[string]bool {
	return func(r *http.Request, issueIDs []pgtype.UUID) map[string]bool {
		allowed := make(map[string]bool, len(issueIDs))
		if len(issueIDs) == 0 {
			return allowed
		}
		member, ok := h.resolveMemberFromRequest(r)
		if !ok {
			return allowed
		}
		wsUUID, err := util.ParseUUID(h.resolveWorkspaceID(r))
		if err != nil {
			return allowed
		}
		// Deduplicate first: one issue can carry several dead runs, and the
		// access check is the expensive part.
		seen := make(map[string]bool, len(issueIDs))
		for _, id := range issueIDs {
			key := uuidToString(id)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          id,
				WorkspaceID: wsUUID,
			})
			if err != nil {
				continue
			}
			if h.canAccessIssue(r.Context(), member, issue) {
				allowed[key] = true
			}
		}
		return allowed
	}
}

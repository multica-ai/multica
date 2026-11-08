package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/service"
)

type IssueLimitUsageResponse struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

// GetIssueLimitUsage returns local usage for a currently enforced Cloud limit.
// Limit mode comes only from Cloud's subscription summary; this endpoint never
// infers unlimited access from cache or refresh reasons.
func (h *Handler) GetIssueLimitUsage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	policy := service.ResolveIssueCountPolicy(r.Context(), h.Entitlements, workspaceID)
	if policy.Action != entitlement.ActionEnforce {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	used, err := service.CountIssueUsage(r.Context(), h.Queries, workspaceID, policy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue limit usage")
		return
	}
	writeJSON(w, http.StatusOK, IssueLimitUsageResponse{Used: used, Limit: policy.Limit})
}

func writeIssueLimitReached(w http.ResponseWriter, err error) bool {
	var limitErr *service.IssueLimitReachedError
	if !errors.As(err, &limitErr) {
		return false
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"code":            "issue_limit_reached",
		"error":           "workspace has reached its issue limit",
		"limit":           limitErr.Limit,
		"policy_revision": limitErr.PolicyRevision,
	})
	return true
}

// writeIssueNumberCollision reports the case the service could not heal: the
// number the workspace counter handed out is already held by another issue, so
// workspace.issue_counter sits below MAX(issue.number). It is a 409 that names
// the constraint and the number, not a 500 — the request was valid, and the
// board is what needs attention. Reaching it means the automatic repair did not
// resolve the drift, so the response also says what to run.
func writeIssueNumberCollision(w http.ResponseWriter, err error) bool {
	var collisionErr *service.IssueNumberCollisionError
	if !errors.As(err, &collisionErr) {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":       "issue_number_collision",
		"error":      "workspace issue counter is behind its highest issue number, so card creation cannot allocate a free number",
		"allocated":  collisionErr.Allocated,
		"constraint": collisionErr.Constraint,
		"repair":     "HealIssueCounter (server/pkg/db/queries/workspace.sql) scoped to this workspace",
	})
	return true
}

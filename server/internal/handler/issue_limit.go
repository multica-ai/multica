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

package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// writeIssueNumberCollision reports a number conflict that automatic repair
// could not resolve and directs the caller to a workspace administrator.
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
		"repair":     "Ask a workspace administrator to repair the issue counter, then retry.",
	})
	return true
}

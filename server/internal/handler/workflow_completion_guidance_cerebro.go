package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// writeWorkflowCompletionGuidance turns a recoverable completion rejection
// into a focused daemon instruction instead of a generic task failure.
func writeWorkflowCompletionGuidance(w http.ResponseWriter, err error) bool {
	guidance, ok := service.WorkflowCompletionGuidanceFromError(err)
	if !ok {
		return false
	}
	writeJSON(w, http.StatusConflict, guidance)
	return true
}

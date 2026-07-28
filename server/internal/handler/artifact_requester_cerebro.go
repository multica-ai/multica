// FIR-3778 — resolve the human an agent is acting for when it creates an
// artifact.
//
// The authenticated user behind an agent run is the runtime owner, which is not
// necessarily the person who asked for the document. The originating task's
// original_user_id is, and it is the same source an issue's on_behalf_of uses
// (see resolveOnBehalfOf in issue.go). Stamping the wrong human here would make
// the edit-access fix admit the wrong person.
package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// resolveArtifactRequester returns the member an agent run traces back to,
// falling back to the authenticated user when the run has no delegating human
// (a schedule, a webhook, an autopilot) or the chain cannot be resolved.
func (h *Handler) resolveArtifactRequester(r *http.Request, userID string) pgtype.UUID {
	taskID := r.Header.Get("X-Task-ID")
	if taskID == "" {
		return parseUUID(userID)
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return parseUUID(userID)
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil || !task.OriginalUserID.Valid {
		return parseUUID(userID)
	}
	return task.OriginalUserID
}

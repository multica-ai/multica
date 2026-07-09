package handler

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolveCoordinatingSquadForIssue returns the temporary coordinating squad
// derived from the issue's latest valid leader task. It never mutates issue
// assignment and never falls back to older squads if the latest leader task no
// longer points at a runnable squad.
func (h *Handler) resolveCoordinatingSquadForIssue(ctx context.Context, issue db.Issue) (db.Squad, bool) {
	squad, err := h.Queries.GetLatestCoordinatingSquadForIssue(ctx, db.GetLatestCoordinatingSquadForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return db.Squad{}, false
	}
	return squad, true
}

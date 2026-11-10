package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// redactHiddenProjectTasks replaces every run on an issue in a private project
// the caller cannot see (MUL-8472) with restrictedTaskResponse. The runs stay
// in the list so an agent's working state and workload counts read the same
// for everyone; only what they were about is removed. tasks and resp are
// parallel slices.
func (h *Handler) redactHiddenProjectTasks(r *http.Request, workspaceID string, tasks []db.AgentTaskQueue, resp []AgentTaskResponse) error {
	hidden, err := h.hiddenProjects(r, workspaceID)
	if err != nil {
		return err
	}
	if hidden.Empty() {
		return nil
	}
	seen := map[pgtype.UUID]struct{}{}
	var issueIDs []pgtype.UUID
	for _, t := range tasks {
		if !t.IssueID.Valid {
			continue
		}
		if _, ok := seen[t.IssueID]; ok {
			continue
		}
		seen[t.IssueID] = struct{}{}
		issueIDs = append(issueIDs, t.IssueID)
	}
	if len(issueIDs) == 0 {
		return nil
	}
	rows, err := h.Queries.ListIssueProjectIDs(r.Context(), issueIDs)
	if err != nil {
		return err
	}
	projectOf := make(map[pgtype.UUID]pgtype.UUID, len(rows))
	for _, row := range rows {
		projectOf[row.ID] = row.ProjectID
	}
	for i, t := range tasks {
		if !t.IssueID.Valid {
			continue
		}
		if hidden.CanSeeProject(projectOf[t.IssueID]) {
			continue
		}
		resp[i] = restrictedTaskResponse(resp[i])
	}
	return nil
}

// restrictedTaskResponse keeps only what agent presence, workload and the
// run-history row shape need. It copies an allowlist instead of clearing a
// denylist so a field added to AgentTaskResponse later is withheld by
// default.
func restrictedTaskResponse(full AgentTaskResponse) AgentTaskResponse {
	return AgentTaskResponse{
		Restricted:    true,
		ID:            full.ID,
		AgentID:       full.AgentID,
		RuntimeID:     full.RuntimeID,
		WorkspaceID:   full.WorkspaceID,
		WorkspaceSlug: full.WorkspaceSlug,
		Status:        full.Status,
		Priority:      full.Priority,
		DispatchedAt:  full.DispatchedAt,
		StartedAt:     full.StartedAt,
		CompletedAt:   full.CompletedAt,
		FailureReason: full.FailureReason,
		Attempt:       full.Attempt,
		MaxAttempts:   full.MaxAttempts,
		ParentTaskID:  full.ParentTaskID,
		IsLeaderTask:  full.IsLeaderTask,
		CreatedAt:     full.CreatedAt,
		Kind:          full.Kind,
		Usage:         full.Usage,
		// Always present on the wire (see the field's comment); empty here.
		DeliveredCommentIDs: []string{},
	}
}

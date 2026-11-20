package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// redactHiddenProjectTasks replaces every run about a private project the
// caller cannot see (MUL-8472) with restrictedTaskResponse: a run on an issue
// there, and an issue-less run (autopilot, quick-create) that reads it. The runs stay
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
	projects, err := projectaccess.TasksProjects(r.Context(), h.Queries, tasks)
	if err != nil {
		return err
	}
	for i := range tasks {
		if !hidden.CanSeeProjects(projects[i]) {
			resp[i] = restrictedTaskResponse(resp[i])
		}
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

// hiddenIssueIDs returns which of issueIDs sit in a project hidden says the
// caller cannot see. Aggregates that name the issues a run is working on use
// it to drop those references while keeping the run itself counted.
func (h *Handler) hiddenIssueIDs(r *http.Request, hidden projectaccess.Hidden, issueIDs []pgtype.UUID) (map[pgtype.UUID]struct{}, error) {
	if hidden.Empty() || len(issueIDs) == 0 {
		return nil, nil
	}
	rows, err := h.Queries.ListIssueProjectIDs(r.Context(), issueIDs)
	if err != nil {
		return nil, err
	}
	out := map[pgtype.UUID]struct{}{}
	for _, row := range rows {
		if !hidden.CanSeeProject(row.ProjectID) {
			out[row.ID] = struct{}{}
		}
	}
	return out, nil
}

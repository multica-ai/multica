package handler

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
)

func (h *Handler) requireBatchSourceChoices(w http.ResponseWriter, r *http.Request, req BatchUpdateIssuesRequest, ws pgtype.UUID) bool {
	if req.DryRun || req.Updates.SourceDecision != "ask" || req.Updates.SuppressRun {
		return true
	}
	for _, id := range req.IssueIDs {
		issueID, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		prev, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: ws})
		if err != nil {
			continue
		}
		if !h.requireProjectVisible(w, r, uuidToString(ws), prev.ProjectID, 404, "project not found") {
			return false
		}
		params := db.UpdateIssueParams{ID: prev.ID, AssigneeType: prev.AssigneeType, AssigneeID: prev.AssigneeID, ProjectID: prev.ProjectID}
		if req.Updates.AssigneeType != nil {
			params.AssigneeType = pgtype.Text{String: *req.Updates.AssigneeType, Valid: *req.Updates.AssigneeType != ""}
		}
		if req.Updates.AssigneeID != nil {
			value, err := util.ParseUUID(*req.Updates.AssigneeID)
			if err != nil {
				return true
			}
			params.AssigneeID = value
		}
		if req.Updates.ProjectID != nil {
			value, err := util.ParseUUID(*req.Updates.ProjectID)
			if err != nil {
				return true
			}
			params.ProjectID = value
		}
		if req.Updates.Status != nil {
			params.Status = pgtype.Text{String: *req.Updates.Status, Valid: true}
		}
		actor, _ := h.resolveActor(r, r.Header.Get("X-User-ID"), uuidToString(ws))
		handoff, err := h.applyIssueWorkflow(r.Context(), prev, &params, req.Updates.Status != nil, req.Updates.AssigneeType != nil || req.Updates.AssigneeID != nil, false, h.agentRunOnIssue(r, actor, prev))
		if err != nil {
			continue
		} // actual per-row validation reports the error, no write here.
		if !h.requireIssueWriteSource(w, r, prev, params, req.Updates, handoff != nil) {
			return false
		}
	}
	return true
}

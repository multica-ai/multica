package handler

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
)

func (h *Handler) requireProjectSourceChoice(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID, ask string, decisions map[string]string, wouldRun bool) bool {
	if !wouldRun || ask != "ask" || !projectID.Valid {
		return true
	}
	actor, _ := h.resolveActor(r, r.Header.Get("X-User-ID"), h.resolveWorkspaceID(r))
	if actor != "member" {
		return true
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return false
	}
	p, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, 500, "Could not load the project's execution source")
		return false
	}
	if !h.requireProjectVisible(w, r, uuidToString(p.WorkspaceID), p.ID, 404, "project not found") {
		return false
	}
	count, err := h.Queries.CountProjectResources(r.Context(), p.ID)
	if err != nil {
		writeError(w, 500, "Could not load the project's execution source")
		return false
	}
	if count > 0 || p.SourceIntent == "no_code" {
		return true
	}
	if decisions[uuidToString(p.ID)] == "no_code_once" {
		return true
	}
	response := projectToResponse(p)
	response.ResourceCount = count
	writeJSON(w, http.StatusConflict, map[string]any{"code": "project_source_required", "error": "Choose the project's execution source before starting this run", "project": response})
	return false
}
func (h *Handler) requireIssueWriteSource(w http.ResponseWriter, r *http.Request, prev db.Issue, params db.UpdateIssueParams, req UpdateIssueRequest, handoff bool) bool {
	candidate := prev
	candidate.AssigneeType = params.AssigneeType
	candidate.AssigneeID = params.AssigneeID
	candidate.ProjectID = params.ProjectID
	if params.Status.Valid {
		candidate.Status = params.Status.String
	}
	actorType, actorID := h.resolveActor(r, r.Header.Get("X-User-ID"), h.resolveWorkspaceID(r))
	_, run := h.IssueService.WillEnqueueRun(r.Context(), service.IssueTriggerInput{Issue: candidate, PrevStatus: prev.Status, AssigneeChanged: candidate.AssigneeID != prev.AssigneeID || candidate.AssigneeType != prev.AssigneeType || handoff, StatusChanged: candidate.Status != prev.Status, EnteredHandoffStep: handoff}, h.issueTriggerWriteProbe(r, actorType, actorID, candidate))
	return h.requireProjectSourceChoice(w, r, candidate.ProjectID, req.SourceDecision, req.SourceDecisions, run && !req.SuppressRun)
}

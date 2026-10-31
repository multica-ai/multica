package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// StageDispatchResponse is a parent issue's stage dispatch: the squad its
// stages go to (null when off) and the member the runs answer to.
type StageDispatchResponse struct {
	IssueID    string  `json:"issue_id"`
	SquadID    *string `json:"squad_id"`
	DispatchBy *string `json:"dispatch_by"`
}

func stageDispatchResponse(issueID pgtype.UUID, row *db.IssueStageDispatch) StageDispatchResponse {
	out := StageDispatchResponse{IssueID: uuidToString(issueID)}
	if row != nil {
		out.SquadID, out.DispatchBy = uuidToPtr(row.SquadID), uuidToPtr(row.DispatchBy)
	}
	return out
}

// StageDispatchService is the server-side stage dispatch wired to this
// handler's issue update path.
func (h *Handler) StageDispatchService() *service.StageDispatchService {
	return &service.StageDispatchService{Issues: h.IssueService, Flags: h.FeatureFlags, Start: h.startStagedIssue}
}

// GetIssueStageDispatch handles GET /api/issues/{id}/stage-dispatch.
func (h *Handler) GetIssueStageDispatch(w http.ResponseWriter, r *http.Request) {
	if !h.StageDispatchService().Enabled(r.Context()) {
		writeError(w, http.StatusNotFound, "stage dispatch is not enabled")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	row, err := h.Queries.GetIssueStageDispatch(r.Context(), db.GetIssueStageDispatchParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusOK, stageDispatchResponse(issue.ID, nil))
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load stage dispatch")
	default:
		writeJSON(w, http.StatusOK, stageDispatchResponse(issue.ID, &row))
	}
}

// SetIssueStageDispatch handles PUT /api/issues/{id}/stage-dispatch with
// {"squad_id": "<id>"} to have the server assign the parent's stages to that
// squad, or {"squad_id": null} to stop. Only a member changes it: the runs it
// starts answer to them, and an agent's run must not undo their choice.
func (h *Handler) SetIssueStageDispatch(w http.ResponseWriter, r *http.Request) {
	svc := h.StageDispatchService()
	if !svc.Enabled(r.Context()) {
		writeError(w, http.StatusNotFound, "stage dispatch is not enabled")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		SquadID *string `json:"squad_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid stage dispatch body")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "only a member can change stage dispatch")
		return
	}
	var squadID, by pgtype.UUID
	if req.SquadID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.SquadID, "squad_id")
		if !ok {
			return
		}
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "squad not found in this workspace")
			return
		}
		if !h.canEnqueueSquadLeader(r.Context(), squad.LeaderID, actorType, actorID, actorID, uuidToString(issue.WorkspaceID)) {
			writeError(w, http.StatusForbidden, "you may not hand work to this squad's leader")
			return
		}
		squadID, by = id, parseUUID(actorID)
	}
	row, err := svc.Set(r.Context(), issue, squadID, by)
	if errors.Is(err, service.ErrStageDispatchInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update stage dispatch")
		return
	}
	writeJSON(w, http.StatusOK, stageDispatchResponse(issue.ID, row))
}

// startStagedIssue opens one sub-issue of the open stage. A write follows
// UpdateIssue for a member (the one who turned dispatch on): the same workflow
// check, status guard, issue:updated payload and run decision, so the
// assignment looks like a person's. It does not process child events: the
// caller's next pass and the scheduler sweep do.
func (h *Handler) startStagedIssue(ctx context.Context, prev db.Issue, cfg db.IssueStageDispatch) (bool, error) {
	actorType, actorID := "member", uuidToString(cfg.DispatchBy)
	ctx = context.WithValue(ctx, wakeupActorKey{}, wakeupActor{kind: actorType, id: actorID})
	assigns := !prev.AssigneeID.Valid
	params := db.UpdateIssueParams{
		ID: prev.ID, ExpectedRevision: pgtype.Int8{Int64: prev.Revision, Valid: true},
		AssigneeType: prev.AssigneeType, AssigneeID: prev.AssigneeID,
		StartDate: prev.StartDate, DueDate: prev.DueDate, ParentIssueID: prev.ParentIssueID,
		ProjectID: prev.ProjectID, Stage: prev.Stage,
	}
	if assigns {
		params.AssigneeType, params.AssigneeID = pgtype.Text{String: "squad", Valid: true}, cfg.SquadID
	}
	leavesBacklog := issuestatus.Effective(ctx, h.Queries, prev.WorkspaceID, prev.Status) == "backlog"
	if !assigns && !leavesBacklog {
		// Assigned ahead of time and already in todo: nothing to write, only
		// the run held back until this stage opened.
		trigger, willRun := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{Issue: prev, AssigneeChanged: true}, service.IssueTriggerProbe{})
		if willRun {
			h.dispatchIssueRun(ctx, prev, trigger, actorType, actorID, "")
		}
		return willRun, nil
	}
	if leavesBacklog {
		params.Status = pgtype.Text{String: "todo", Valid: true}
	}
	handoff, err := h.applyIssueWorkflow(ctx, prev, &params, leavesBacklog, assigns, false, nil)
	var notIn *issueworkflow.NotInWorkflowError
	if errors.As(err, &notIn) {
		slog.Warn("stage dispatch: the project workflow has no todo status; sub-issue left in place",
			"issue_id", uuidToString(prev.ID), "workflow", notIn.WorkflowName)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	writeCtx := withWakeupHandedTo(ctx, h.handedToAgent(ctx, prev, params, handoff != nil))
	issue, err := h.updateIssueWithStatusGuard(writeCtx, prev.WorkspaceID, params.Status.String, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // changed since this pass read it
	}
	if err != nil {
		return false, err
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, &resp)
	assigneeChanged := prev.AssigneeType.String != issue.AssigneeType.String || prev.AssigneeID != issue.AssigneeID
	statusChanged := prev.Status != issue.Status
	trigger, willRun := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{
		Issue: issue, PrevStatus: prev.Status, AssigneeChanged: assigneeChanged || handoff != nil,
		StatusChanged: statusChanged, EnteredHandoffStep: handoff != nil,
	}, service.IssueTriggerProbe{})
	payload := map[string]any{
		"issue":                      resp,
		"assignee_changed":           assigneeChanged,
		"status_changed":             statusChanged,
		"prev_assignee_type":         textToPtr(prev.AssigneeType),
		"prev_assignee_id":           uuidToPtr(prev.AssigneeID),
		"prev_status":                prev.Status,
		"creator_type":               prev.CreatorType,
		"creator_id":                 uuidToString(prev.CreatorID),
		"duplicate_of_issue_id":      liveDuplicateMark(issue.Status, issue.DuplicateOfIssueID),
		"prev_duplicate_of_issue_id": liveDuplicateMark(prev.Status, prev.DuplicateOfIssueID),
	}
	if handoff != nil {
		payload["workflow_handoff"] = workflowHandoffPayload(issue, handoff, h.workflowHandoffBrief(ctx, issue, handoff), willRun)
	}
	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, payload)
	if willRun {
		h.dispatchIssueRun(ctx, issue, trigger, actorType, actorID, "")
	}
	slog.Info("stage dispatch: sub-issue opened",
		"parent_id", uuidToString(cfg.IssueID), "issue_id", uuidToString(issue.ID), "stage", issue.Stage.Int32, "run", willRun)
	return willRun, nil
}

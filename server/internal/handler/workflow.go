package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type workflowDefinitionResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Version     int32           `json:"version"`
	Definition  json.RawMessage `json:"definition"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
}

type workflowRunResponse struct {
	ID                   string          `json:"id"`
	WorkspaceID          string          `json:"workspace_id"`
	IssueID              string          `json:"issue_id"`
	WorkflowDefinitionID string          `json:"workflow_definition_id"`
	DefinitionSnapshot   json.RawMessage `json:"definition_snapshot"`
	Status               string          `json:"status"`
	CurrentStage         int32           `json:"current_stage"`
	Revision             int64           `json:"revision"`
	StartedByType        string          `json:"started_by_type"`
	StartedByID          string          `json:"started_by_id"`
	StartedAt            time.Time       `json:"started_at"`
	CompletedAt          *time.Time      `json:"completed_at,omitempty"`
	CancelledAt          *time.Time      `json:"cancelled_at,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type workflowTransitionResponse struct {
	ID             string          `json:"id"`
	WorkflowRunID  string          `json:"workflow_run_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Kind           string          `json:"kind"`
	FromStage      *int32          `json:"from_stage,omitempty"`
	ToStage        *int32          `json:"to_stage,omitempty"`
	FromStatus     string          `json:"from_status,omitempty"`
	ToStatus       string          `json:"to_status"`
	ActorType      string          `json:"actor_type"`
	ActorID        string          `json:"actor_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
}

type workflowMutationResponse struct {
	Run         workflowRunResponse          `json:"run"`
	Transitions []workflowTransitionResponse `json:"transitions"`
	Outcome     string                       `json:"outcome"`
}

type createWorkflowDefinitionRequest struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

type startIssueWorkflowRequest struct {
	WorkflowDefinitionID string `json:"workflow_definition_id"`
}

func workflowDefinitionToResponse(row db.WorkflowDefinition) workflowDefinitionResponse {
	return workflowDefinitionResponse{
		ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), Name: row.Name, Version: row.Version,
		Definition: json.RawMessage(append([]byte(nil), row.Definition...)), CreatedBy: uuidToString(row.CreatedBy), CreatedAt: row.CreatedAt.Time,
	}
}

func workflowRunToResponse(row db.WorkflowRun) workflowRunResponse {
	return workflowRunResponse{
		ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), IssueID: uuidToString(row.IssueID),
		WorkflowDefinitionID: uuidToString(row.WorkflowDefinitionID),
		DefinitionSnapshot:   json.RawMessage(append([]byte(nil), row.DefinitionSnapshot...)),
		Status:               row.Status, CurrentStage: row.CurrentStage, Revision: row.Revision,
		StartedByType: row.StartedByType, StartedByID: uuidToString(row.StartedByID), StartedAt: row.StartedAt.Time,
		CompletedAt: workflowTimePtr(row.CompletedAt), CancelledAt: workflowTimePtr(row.CancelledAt),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func workflowTransitionToResponse(row db.WorkflowTransition) workflowTransitionResponse {
	return workflowTransitionResponse{
		ID: uuidToString(row.ID), WorkflowRunID: uuidToString(row.WorkflowRunID), IdempotencyKey: row.IdempotencyKey,
		Kind: row.Kind, FromStage: workflowInt4Ptr(row.FromStage), ToStage: workflowInt4Ptr(row.ToStage),
		FromStatus: row.FromStatus.String, ToStatus: row.ToStatus, ActorType: row.ActorType,
		ActorID: uuidToString(row.ActorID), Payload: json.RawMessage(append([]byte(nil), row.Payload...)), CreatedAt: row.CreatedAt.Time,
	}
}

func workflowMutationToResponse(result service.WorkflowMutationResult) workflowMutationResponse {
	transitions := make([]workflowTransitionResponse, 0, len(result.Transitions))
	for _, row := range result.Transitions {
		transitions = append(transitions, workflowTransitionToResponse(row))
	}
	return workflowMutationResponse{Run: workflowRunToResponse(result.Run), Transitions: transitions, Outcome: result.Outcome}
}

func workflowTimePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func workflowInt4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

func (h *Handler) workflowWorkspace(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return "", pgtype.UUID{}, false
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return "", pgtype.UUID{}, false
	}
	return workspaceID, workspaceUUID, true
}

func (h *Handler) workflowActor(w http.ResponseWriter, r *http.Request, workspaceID string) (service.WorkflowActor, string, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return service.WorkflowActor{}, "", "", false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid request actor")
		return service.WorkflowActor{}, "", "", false
	}
	return service.WorkflowActor{Type: actorType, ID: actorUUID}, actorType, actorID, true
}

func writeWorkflowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidWorkflowDefinition):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrWorkflowDefinitionNotFound), errors.Is(err, service.ErrWorkflowRunNotFound), errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "workflow resource not found")
	case errors.Is(err, service.ErrActiveWorkflowRun), errors.Is(err, service.ErrWorkflowConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("workflow request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "workflow request failed")
	}
}

func (h *Handler) CreateWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	_, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	createdBy, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user id")
		return
	}
	var req createWorkflowDefinitionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow definition request")
		return
	}
	created, err := h.WorkflowService.CreateDefinition(r.Context(), service.CreateWorkflowDefinitionParams{
		WorkspaceID: workspaceUUID, Name: req.Name, Definition: req.Definition, CreatedBy: createdBy,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workflowDefinitionToResponse(created))
}

func (h *Handler) ListWorkflowDefinitions(w http.ResponseWriter, r *http.Request) {
	_, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	rows, err := h.WorkflowService.ListLatestDefinitions(r.Context(), workspaceUUID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]workflowDefinitionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowDefinitionToResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) GetWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	_, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow definition id")
		return
	}
	row, err := h.WorkflowService.GetDefinition(r.Context(), workspaceUUID, id)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowDefinitionToResponse(row))
}

func (h *Handler) workflowIssueID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (h *Handler) GetIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	_, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	issueID, ok := h.workflowIssueID(w, r)
	if !ok {
		return
	}
	run, err := h.WorkflowService.GetCurrentOrLatestRun(r.Context(), workspaceUUID, issueID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowRunToResponse(run))
}

func (h *Handler) StartIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	issueID, ok := h.workflowIssueID(w, r)
	if !ok {
		return
	}
	actor, actorType, actorID, ok := h.workflowActor(w, r, workspaceID)
	if !ok {
		return
	}
	var req startIssueWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow start request")
		return
	}
	definitionID, err := util.ParseUUID(req.WorkflowDefinitionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow definition id")
		return
	}
	result, err := h.WorkflowService.Start(r.Context(), service.StartWorkflowParams{
		WorkspaceID: workspaceUUID, IssueID: issueID, DefinitionID: definitionID, Actor: actor,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	h.applyWorkflowMutationSideEffects(r.Context(), r, result, actorType, actorID)
	h.logWorkflowTransitions(result)
	writeJSON(w, http.StatusOK, workflowMutationToResponse(result))
}

func (h *Handler) ResumeIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	issueID, ok := h.workflowIssueID(w, r)
	if !ok {
		return
	}
	actor, actorType, actorID, ok := h.workflowActor(w, r, workspaceID)
	if !ok {
		return
	}
	result, err := h.WorkflowService.Resume(r.Context(), service.ResumeWorkflowParams{
		WorkspaceID: workspaceUUID, IssueID: issueID, Actor: actor,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	h.applyWorkflowMutationSideEffects(r.Context(), r, result, actorType, actorID)
	h.logWorkflowTransitions(result)
	writeJSON(w, http.StatusOK, workflowMutationToResponse(result))
}

func (h *Handler) CancelIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	issueID, ok := h.workflowIssueID(w, r)
	if !ok {
		return
	}
	actor, _, _, ok := h.workflowActor(w, r, workspaceID)
	if !ok {
		return
	}
	result, err := h.WorkflowService.Cancel(r.Context(), workspaceUUID, issueID, actor)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	h.logWorkflowTransitions(result)
	writeJSON(w, http.StatusOK, workflowMutationToResponse(result))
}

func (h *Handler) ListIssueWorkflowTransitions(w http.ResponseWriter, r *http.Request) {
	_, workspaceUUID, ok := h.workflowWorkspace(w, r)
	if !ok {
		return
	}
	issueID, ok := h.workflowIssueID(w, r)
	if !ok {
		return
	}
	run, err := h.WorkflowService.GetCurrentOrLatestRun(r.Context(), workspaceUUID, issueID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	rows, err := h.WorkflowService.ListTransitions(r.Context(), workspaceUUID, run.ID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]workflowTransitionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowTransitionToResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) applyWorkflowMutationSideEffects(
	ctx context.Context,
	req *http.Request,
	result service.WorkflowMutationResult,
	actorType, actorID string,
) {
	for _, change := range result.Changes {
		after := change.After
		workspaceID := uuidToString(after.WorkspaceID)
		prefix := h.getIssuePrefix(ctx, after.WorkspaceID)
		resp := issueToResponse(after, prefix)
		h.fillStatusCategory(ctx, after.WorkspaceID, &resp)
		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
			"issue": resp, "status_changed": true, "prev_status": change.Before.Status,
			"assignee_changed": false, "priority_changed": false, "project_changed": false,
		})
		if after.Status != "todo" {
			continue
		}
		probe := service.IssueTriggerProbe{}
		if req != nil {
			probe = h.issueTriggerWriteProbe(req, actorType, actorID, after)
		}
		trigger, shouldRun := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{
			Issue: after, PrevStatus: change.Before.Status, StatusChanged: true,
		}, probe)
		if shouldRun {
			h.dispatchIssueRun(ctx, after, trigger, actorType, actorID, "")
		}
	}
}

func (h *Handler) logWorkflowTransitions(result service.WorkflowMutationResult) {
	for _, transition := range result.Transitions {
		slog.Info("workflow transition",
			"workflow_run_id", uuidToString(result.Run.ID),
			"workflow_definition_id", uuidToString(result.Run.WorkflowDefinitionID),
			"issue_id", uuidToString(result.Run.IssueID), "workspace_id", uuidToString(result.Run.WorkspaceID),
			"kind", transition.Kind,
			"from_stage", workflowLogStage(transition.FromStage),
			"to_stage", workflowLogStage(transition.ToStage),
			"from_status", transition.FromStatus.String,
			"to_status", transition.ToStatus,
		)
	}
}

func workflowLogStage(v pgtype.Int4) any {
	if !v.Valid {
		return nil
	}
	return v.Int32
}

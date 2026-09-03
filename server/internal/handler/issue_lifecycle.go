package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type issueLifecycleDefinitionResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	Name        string `json:"name"`
	Revision    int64  `json:"revision"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type issueLifecycleStatusResponse struct {
	ID                  string                     `json:"id"`
	LifecycleID         string                     `json:"lifecycle_id"`
	LegacyStatusKey     *string                    `json:"legacy_status_key"`
	Name                string                     `json:"name"`
	Description         string                     `json:"description"`
	Color               string                     `json:"color"`
	Position            float64                    `json:"position"`
	Phase               string                     `json:"phase"`
	Outcome             *string                    `json:"outcome"`
	EntryPolicy         issuelifecycle.EntryPolicy `json:"entry_policy"`
	EntryPolicyRevision int64                      `json:"entry_policy_revision"`
	ArchivedAt          *string                    `json:"archived_at"`
	CreatedAt           string                     `json:"created_at"`
	UpdatedAt           string                     `json:"updated_at"`
}

type issueLifecycleResponse struct {
	Lifecycle issueLifecycleDefinitionResponse `json:"lifecycle"`
	Statuses  []issueLifecycleStatusResponse   `json:"statuses"`
	Mode      string                           `json:"mode"`
}

type automationExecutionResponse struct {
	ID                  string                     `json:"id"`
	IssueID             string                     `json:"issue_id"`
	TriggerTransitionID string                     `json:"trigger_transition_id"`
	LifecycleID         string                     `json:"lifecycle_id"`
	LifecycleRevision   int64                      `json:"lifecycle_revision"`
	StatusID            string                     `json:"status_id"`
	PolicyRevision      int64                      `json:"policy_revision"`
	PolicySnapshot      issuelifecycle.EntryPolicy `json:"policy_snapshot"`
	ExecutorType        *string                    `json:"executor_type"`
	ExecutorID          *string                    `json:"executor_id"`
	Status              string                     `json:"status"`
	CreatedAt           string                     `json:"created_at"`
	UpdatedAt           string                     `json:"updated_at"`
}

func automationExecutionToResponse(execution db.AutomationExecution) automationExecutionResponse {
	policy, err := issuelifecycle.DecodeEntryPolicy(execution.PolicySnapshot)
	if err != nil {
		policy = issuelifecycle.DefaultEntryPolicy()
	}
	return automationExecutionResponse{
		ID: uuidToString(execution.ID), IssueID: uuidToString(execution.IssueID),
		TriggerTransitionID: uuidToString(execution.TriggerTransitionID),
		LifecycleID:         uuidToString(execution.LifecycleID), LifecycleRevision: execution.LifecycleRevision,
		StatusID: uuidToString(execution.StatusID), PolicyRevision: execution.PolicyRevision,
		PolicySnapshot: policy, ExecutorType: textToPtr(execution.ExecutorType),
		ExecutorID: uuidToPtr(execution.ExecutorID), Status: execution.Status,
		CreatedAt: timestampToString(execution.CreatedAt), UpdatedAt: timestampToString(execution.UpdatedAt),
	}
}

func lifecycleDefinitionToResponse(lifecycle db.IssueLifecycle) issueLifecycleDefinitionResponse {
	return issueLifecycleDefinitionResponse{
		ID:          uuidToString(lifecycle.ID),
		WorkspaceID: uuidToString(lifecycle.WorkspaceID),
		ScopeType:   lifecycle.ScopeType,
		ScopeID:     uuidToString(lifecycle.ScopeID),
		Name:        lifecycle.Name,
		Revision:    lifecycle.Revision,
		CreatedAt:   timestampToString(lifecycle.CreatedAt),
		UpdatedAt:   timestampToString(lifecycle.UpdatedAt),
	}
}

func lifecycleStatusToResponse(status db.IssueLifecycleStatus) issueLifecycleStatusResponse {
	policy, err := issuelifecycle.DecodeEntryPolicy(status.EntryPolicy)
	if err != nil {
		policy = issuelifecycle.DefaultEntryPolicy()
	}
	return issueLifecycleStatusResponse{
		ID:                  uuidToString(status.ID),
		LifecycleID:         uuidToString(status.LifecycleID),
		LegacyStatusKey:     textToPtr(status.LegacyStatusKey),
		Name:                status.Name,
		Description:         status.Description,
		Color:               status.Color,
		Position:            status.Position,
		Phase:               status.Phase,
		Outcome:             textToPtr(status.Outcome),
		EntryPolicy:         policy,
		EntryPolicyRevision: status.EntryPolicyRevision,
		ArchivedAt:          timestampToPtr(status.ArchivedAt),
		CreatedAt:           timestampToString(status.CreatedAt),
		UpdatedAt:           timestampToString(status.UpdatedAt),
	}
}

func buildIssueLifecycleResponse(lifecycle db.IssueLifecycle, statuses []db.IssueLifecycleStatus, projectID pgtype.UUID) issueLifecycleResponse {
	mode := "default"
	if projectID.Valid && lifecycle.ScopeType == "project" && lifecycle.ScopeID == projectID {
		mode = "custom"
	}
	statusResponses := make([]issueLifecycleStatusResponse, len(statuses))
	for i := range statuses {
		statusResponses[i] = lifecycleStatusToResponse(statuses[i])
	}
	return issueLifecycleResponse{
		Lifecycle: lifecycleDefinitionToResponse(lifecycle),
		Statuses:  statusResponses,
		Mode:      mode,
	}
}

// GetEffectiveIssueLifecycle returns the definition a newly-created issue will
// bind to. Existing issues remain pinned to the lifecycle_id in their own row.
func (h *Handler) GetEffectiveIssueLifecycle(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	var projectID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		projectID, ok = parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: projectID, WorkspaceID: wsUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "project not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load project")
			return
		}
	}

	lifecycle, err := issuelifecycle.Effective(r.Context(), h.Queries, wsUUID, projectID)
	if err != nil {
		slog.Warn("get effective issue lifecycle failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load issue lifecycle")
		return
	}
	statuses, err := h.Queries.ListIssueLifecycleStatuses(r.Context(), db.ListIssueLifecycleStatusesParams{
		WorkspaceID:     wsUUID,
		LifecycleID:     lifecycle.ID,
		IncludeArchived: strings.EqualFold(r.URL.Query().Get("include_archived"), "true"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle statuses")
		return
	}
	writeJSON(w, http.StatusOK, buildIssueLifecycleResponse(lifecycle, statuses, projectID))
}

// GetIssueLifecycle returns a concrete pinned definition. Unlike the
// effective endpoint it is safe for existing issues after their project has
// switched between inherited and custom modes.
func (h *Handler) GetIssueLifecycle(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	lifecycleID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "lifecycleId"), "lifecycle id")
	if !ok {
		return
	}
	lifecycle, err := h.Queries.GetIssueLifecycleByID(r.Context(), db.GetIssueLifecycleByIDParams{
		ID: lifecycleID, WorkspaceID: workspaceUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue lifecycle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load issue lifecycle")
		return
	}
	response, err := lifecycleResponseForRequest(r, h.Queries, workspaceUUID, lifecycle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue lifecycle statuses")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type updateProjectIssueLifecycleRequest struct {
	Mode string `json:"mode"`
}

// UpdateProjectIssueLifecycle switches a project between inherited and custom
// configuration. It never rewrites existing issue bindings.
func (h *Handler) UpdateProjectIssueLifecycle(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	var req updateProjectIssueLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != "default" && req.Mode != "custom" {
		writeError(w, http.StatusBadRequest, "mode must be default or custom")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project lifecycle")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if req.Mode == "custom" {
		_, err = issuelifecycle.CustomizeProject(r.Context(), qtx, wsUUID, projectID)
	} else {
		err = issuelifecycle.UseWorkspaceDefault(r.Context(), qtx, wsUUID, projectID)
	}
	if err != nil {
		slog.Warn("update project issue lifecycle failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update project lifecycle")
		return
	}
	lifecycle, err := issuelifecycle.Effective(r.Context(), qtx, wsUUID, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload project lifecycle")
		return
	}
	statuses, err := qtx.ListIssueLifecycleStatuses(r.Context(), db.ListIssueLifecycleStatusesParams{
		WorkspaceID: wsUUID, LifecycleID: lifecycle.ID, IncludeArchived: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload lifecycle statuses")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project lifecycle")
		return
	}
	h.publish(protocol.EventIssueStatusChanged, workspaceID, "member", requestUserID(r), map[string]any{
		"action": "lifecycle_mode_changed", "project_id": uuidToString(projectID),
	})
	writeJSON(w, http.StatusOK, buildIssueLifecycleResponse(lifecycle, statuses, projectID))
}

type transitionIssueStatusNodeRequest struct {
	LifecycleStatusID    string  `json:"lifecycle_status_id"`
	ExpectedRevision     *int64  `json:"expected_revision,omitempty"`
	ExpectedTransitionID *string `json:"expected_transition_id,omitempty"`
}

type transitionIssueStatusNodeResponse struct {
	Issue      IssueResponse                   `json:"issue"`
	Transition *issueTransitionHistoryResponse `json:"transition"`
	Execution  *automationExecutionResponse    `json:"execution"`
	TaskID     *string                         `json:"task_id"`
}

type issueTransitionHistoryResponse struct {
	ID                  string  `json:"id"`
	FromStatusID        *string `json:"from_status_id"`
	ToStatusID          string  `json:"to_status_id"`
	ActorType           string  `json:"actor_type"`
	ActorID             *string `json:"actor_id"`
	Cause               string  `json:"cause"`
	IssueRevisionBefore int64   `json:"issue_revision_before"`
	IssueRevisionAfter  int64   `json:"issue_revision_after"`
	CreatedAt           string  `json:"created_at"`
}

func transitionHistoryToResponse(transition db.IssueTransition) issueTransitionHistoryResponse {
	return issueTransitionHistoryResponse{
		ID:                  uuidToString(transition.ID),
		FromStatusID:        uuidToPtr(transition.FromStatusID),
		ToStatusID:          uuidToString(transition.ToStatusID),
		ActorType:           transition.ActorType,
		ActorID:             uuidToPtr(transition.ActorID),
		Cause:               transition.Cause,
		IssueRevisionBefore: transition.IssueRevisionBefore,
		IssueRevisionAfter:  transition.IssueRevisionAfter,
		CreatedAt:           timestampToString(transition.CreatedAt),
	}
}

// TransitionIssueStatusNode is the lifecycle-native status mutation used by
// new clients. Legacy status-key mutations remain as a compatibility adapter.
func (h *Handler) TransitionIssueStatusNode(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req transitionIssueStatusNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	statusID, ok := parseUUIDOrBadRequest(w, req.LifecycleStatusID, "lifecycle_status_id")
	if !ok {
		return
	}
	params := service.IssueStatusNodeTransitionParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, LifecycleStatusID: statusID,
		Cause: "status_node_transition",
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	params.Actor.Type = actorType
	if actorID != "" {
		params.Actor.ID, _ = util.ParseUUID(actorID)
	}
	if req.ExpectedRevision != nil {
		if *req.ExpectedRevision < 1 {
			writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
			return
		}
		params.ExpectedRevision = pgtype.Int8{Int64: *req.ExpectedRevision, Valid: true}
	}
	if req.ExpectedTransitionID != nil {
		params.ExpectedTransitionID, ok = parseUUIDOrBadRequest(w, *req.ExpectedTransitionID, "expected_transition_id")
		if !ok {
			return
		}
	}

	result, err := h.IssueService.TransitionStatusNode(r.Context(), params)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIssueTransitionConflict):
			writeError(w, http.StatusConflict, "issue transition conflict; reload and retry")
		case errors.Is(err, service.ErrIssueTransitionStatusUnavailable):
			writeError(w, http.StatusConflict, "lifecycle status is unavailable")
		case errors.Is(err, service.ErrIssueEntryPolicyExecutorUnavailable), errors.Is(err, service.ErrIssueEntryPolicyAssigneeUnavailable):
			writeError(w, http.StatusConflict, err.Error())
		default:
			slog.Warn("transition issue lifecycle status failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to transition issue")
		}
		return
	}
	resp := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	if result.Changed {
		assigneeChanged := result.Previous.AssigneeType != result.Issue.AssigneeType || result.Previous.AssigneeID != result.Issue.AssigneeID
		payload := map[string]any{
			"issue": resp, "status_changed": true, "prev_status": result.Previous.Status,
			"prev_status_name": result.PreviousStatusName, "status_name": result.StatusName,
			"assignee_changed": assigneeChanged,
			"transition":       transitionHistoryToResponse(result.Transition),
		}
		h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, payload)
		h.publish(protocol.EventIssueTransitioned, uuidToString(issue.WorkspaceID), actorType, actorID, payload)
	}
	response := transitionIssueStatusNodeResponse{Issue: resp}
	if result.Changed {
		transition := transitionHistoryToResponse(result.Transition)
		response.Transition = &transition
		execution := automationExecutionToResponse(result.Execution)
		response.Execution = &execution
		response.TaskID = uuidToPtr(result.Task.ID)
	}
	writeJSON(w, http.StatusOK, response)
}

// ListIssueAutomationExecutions exposes the immutable policy snapshots and
// current execution states used by the issue activity UI and recovery tools.
func (h *Handler) ListIssueAutomationExecutions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	executions, err := h.Queries.ListIssueAutomationExecutions(r.Context(), db.ListIssueAutomationExecutionsParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load automation executions")
		return
	}
	response := make([]automationExecutionResponse, len(executions))
	for i := range executions {
		response[i] = automationExecutionToResponse(executions[i])
	}
	writeJSON(w, http.StatusOK, response)
}

type takeOverAutomationExecutionRequest struct {
	ExpectedRevision *int64 `json:"expected_revision,omitempty"`
}

type takeOverAutomationExecutionResponse struct {
	Issue     IssueResponse               `json:"issue"`
	Execution automationExecutionResponse `json:"execution"`
}

func (h *Handler) TakeOverAutomationExecution(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(issue.WorkspaceID), "workspace not found")
	if !ok {
		return
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "automation execution id")
	if !ok {
		return
	}
	var req takeOverAutomationExecutionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	params := service.IssueAutomationTakeoverParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, ExecutionID: executionID, MemberID: member.UserID,
	}
	if req.ExpectedRevision != nil {
		params.ExpectedRevision = pgtype.Int8{Int64: *req.ExpectedRevision, Valid: true}
	}
	result, err := h.IssueService.TakeOverAutomationExecution(r.Context(), params)
	if err != nil {
		if errors.Is(err, service.ErrIssueTransitionConflict) {
			writeError(w, http.StatusConflict, "automation execution changed; reload and retry")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to take over automation execution")
		return
	}
	responseIssue := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &responseIssue)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
		"issue": responseIssue, "assignee_changed": true, "automation_taken_over": true,
	})
	writeJSON(w, http.StatusOK, takeOverAutomationExecutionResponse{
		Issue: responseIssue, Execution: automationExecutionToResponse(result.Execution),
	})
}

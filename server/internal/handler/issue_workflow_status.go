package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type updateIssueWorkflowStatusRequest struct {
	ExpectedRevision int64                      `json:"expected_revision"`
	Name             *string                    `json:"name"`
	Description      *string                    `json:"description"`
	Color            *string                    `json:"color"`
	Phase            *string                    `json:"phase"`
	EntryPolicy      *issueworkflow.EntryPolicy `json:"entry_policy"`
}

type reorderIssueWorkflowStatusesRequest struct {
	ExpectedRevision int64    `json:"expected_revision"`
	StatusIDs        []string `json:"status_ids"`
}

func workflowOutcome(phase string) (pgtype.Text, bool) {
	switch phase {
	case issueworkflow.PhaseBacklog, issueworkflow.PhaseUnstarted, issueworkflow.PhaseStarted:
		return pgtype.Text{}, true
	case issueworkflow.PhaseCompleted:
		return pgtype.Text{String: "completed", Valid: true}, true
	case issueworkflow.PhaseCancelled:
		return pgtype.Text{String: "cancelled", Valid: true}, true
	default:
		return pgtype.Text{}, false
	}
}

func workflowProjectID(workflow db.IssueWorkflow) pgtype.UUID {
	if workflow.ScopeType == "project" {
		return workflow.ScopeID
	}
	return pgtype.UUID{}
}

func (h *Handler) workflowMutationContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workflowId"), "workflow id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	return workspaceUUID, workflowID, member, true
}

func (h *Handler) validateEntryPolicyReferences(w http.ResponseWriter, r *http.Request, workspaceID string, policy issueworkflow.EntryPolicy) bool {
	validate := func(principal issueworkflow.EntryPolicyPrincipal, executor bool) bool {
		if principal.Type == issueworkflow.AssigneeKeep || principal.Type == issueworkflow.ExecutorNone {
			return true
		}
		id, ok := parseUUIDOrBadRequest(w, principal.ID, "entry policy principal id")
		if !ok {
			return false
		}
		principalType := principal.Type
		if principalType == issueworkflow.AssigneeHuman {
			if executor {
				writeError(w, http.StatusBadRequest, "executor.type must be none, agent, or squad")
				return false
			}
			principalType = "member"
		}
		if status, message := h.validateAssigneePair(r.Context(), r, workspaceID,
			pgtype.Text{String: principalType, Valid: true}, id); status != 0 {
			writeError(w, status, message)
			return false
		}
		return true
	}
	return validate(policy.Assignee, false) && validate(policy.Executor, true)
}

func (h *Handler) publishIssueWorkflowChanged(workspaceID string, member db.Member, action string, workflowID pgtype.UUID) {
	h.publish(protocol.EventIssueStatusChanged, workspaceID, "member", uuidToString(member.UserID), map[string]any{
		"action": action, "workflow_id": uuidToString(workflowID),
	})
}

func workflowResponseForRequest(r *http.Request, q *db.Queries, workspaceID pgtype.UUID, workflow db.IssueWorkflow) (issueWorkflowResponse, error) {
	statuses, err := q.ListIssueWorkflowStatuses(r.Context(), db.ListIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID, IncludeArchived: true,
	})
	if err != nil {
		return issueWorkflowResponse{}, err
	}
	return buildIssueWorkflowResponse(workflow, statuses, workflowProjectID(workflow)), nil
}

// UpdateIssueWorkflowStatus edits one active stable status node. The
// workflow row lock serializes definition revisions; entry_policy_revision
// advances independently and only when the normalized policy changes.
func (h *Handler) UpdateIssueWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, workflowID, member, ok := h.workflowMutationContext(w, r)
	if !ok {
		return
	}
	statusID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "statusId"), "workflow status id")
	if !ok {
		return
	}
	var req updateIssueWorkflowStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return
	}
	if req.Name == nil && req.Description == nil && req.Color == nil && req.Phase == nil && req.EntryPolicy == nil {
		writeError(w, http.StatusBadRequest, "at least one workflow status field is required")
		return
	}

	var requestedPolicy []byte
	var normalizedPolicy issueworkflow.EntryPolicy
	if req.EntryPolicy != nil {
		var err error
		requestedPolicy, normalizedPolicy, err = issueworkflow.EncodeEntryPolicy(*req.EntryPolicy)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !h.validateEntryPolicyReferences(w, r, uuidToString(workspaceID), normalizedPolicy) {
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow status")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	workflow, err := qtx.LockEditableIssueWorkflow(r.Context(), db.LockEditableIssueWorkflowParams{
		WorkflowID: workflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "workflow is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workflow")
		return
	}
	if workflow.Revision != req.ExpectedRevision {
		writeError(w, http.StatusConflict, "workflow revision changed; reload and retry")
		return
	}
	current, err := qtx.GetIssueWorkflowStatusByID(r.Context(), db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID, WorkflowID: workflowID, ID: statusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load workflow status")
		return
	}
	if current.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived workflow statuses cannot be modified")
		return
	}
	active, err := qtx.ListActiveIssueWorkflowStatuses(r.Context(), db.ListActiveIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflowID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow statuses")
		return
	}

	if req.EntryPolicy != nil && normalizedPolicy.NextStatusKey != "" {
		found := false
		for _, status := range active {
			if status.ID != current.ID && status.SpecKey == normalizedPolicy.NextStatusKey {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "next_status_key must reference another active status in this workflow")
			return
		}
	}

	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 64 {
			writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
			return
		}
	}
	for _, status := range active {
		if status.ID != current.ID && strings.EqualFold(strings.TrimSpace(status.Name), name) {
			writeError(w, http.StatusConflict, "an active workflow status with this name already exists")
			return
		}
	}
	description := current.Description
	if req.Description != nil {
		description = *req.Description
		if len([]rune(description)) > 256 {
			writeError(w, http.StatusBadRequest, "description must be at most 256 characters")
			return
		}
	}
	color := current.Color
	if req.Color != nil {
		color, err = normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		color = strings.ToLower(color)
	}
	position := current.Position
	phase := current.Phase
	if req.Phase != nil {
		phase = strings.TrimSpace(*req.Phase)
	}
	outcome, validPhase := workflowOutcome(phase)
	if !validPhase {
		writeError(w, http.StatusBadRequest, "phase must be backlog, unstarted, started, completed, or cancelled")
		return
	}

	entryPolicy := current.EntryPolicy
	policyChanged := false
	if req.EntryPolicy != nil {
		storedPolicy, decodeErr := issueworkflow.DecodeEntryPolicy(current.EntryPolicy)
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "stored entry policy is invalid")
			return
		}
		policyChanged = storedPolicy != normalizedPolicy
		if policyChanged {
			entryPolicy = requestedPolicy
		}
	}
	changed := name != current.Name || description != current.Description || color != current.Color || position != current.Position || phase != current.Phase || policyChanged
	if !changed {
		response, listErr := workflowResponseForRequest(r, qtx, workspaceID, workflow)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload workflow")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	if _, err = qtx.UpdateIssueWorkflowStatusDefinition(r.Context(), db.UpdateIssueWorkflowStatusDefinitionParams{
		Name: name, Description: description, Color: color, Position: position,
		Phase: phase, Outcome: outcome, EntryPolicy: entryPolicy,
		BumpEntryPolicyRevision: policyChanged, StatusID: statusID,
		WorkspaceID: workspaceID, WorkflowID: workflowID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "workflow status is no longer editable")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a workflow status with this name already exists")
			return
		}
		slog.Warn("update workflow status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update workflow status")
		return
	}
	workflow, err = qtx.BumpIssueWorkflowRevision(r.Context(), db.BumpIssueWorkflowRevisionParams{
		ID: workflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to version workflow")
		return
	}
	response, err := workflowResponseForRequest(r, qtx, workspaceID, workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload workflow")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow status")
		return
	}
	h.publishIssueWorkflowChanged(uuidToString(workspaceID), member, "workflow_status_updated", workflowID)
	writeJSON(w, http.StatusOK, response)
}

// ArchiveIssueWorkflowStatus retires a node from future transitions while
// preserving existing issue bindings and transition history.
func (h *Handler) ArchiveIssueWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, workflowID, member, ok := h.workflowMutationContext(w, r)
	if !ok {
		return
	}
	expectedRevision, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("expected_revision")), 10, 64)
	if err != nil || expectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return
	}
	statusID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "statusId"), "workflow status id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive workflow status")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	workflow, err := qtx.LockEditableIssueWorkflow(r.Context(), db.LockEditableIssueWorkflowParams{
		WorkflowID: workflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "workflow is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workflow")
		return
	}
	if workflow.Revision != expectedRevision {
		writeError(w, http.StatusConflict, "workflow revision changed; reload and retry")
		return
	}
	current, err := qtx.GetIssueWorkflowStatusByID(r.Context(), db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID, WorkflowID: workflowID, ID: statusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load workflow status")
		return
	}
	if current.ArchivedAt.Valid {
		response, listErr := workflowResponseForRequest(r, qtx, workspaceID, workflow)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload workflow")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if workflow.InitialStatusID == current.ID {
		writeError(w, http.StatusConflict, "the initial workflow status cannot be archived; choose a new initial status first")
		return
	}
	active, err := qtx.ListActiveIssueWorkflowStatuses(r.Context(), db.ListActiveIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflowID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow statuses")
		return
	}
	if len(active) <= 1 {
		writeError(w, http.StatusConflict, "a workflow must keep at least one active status")
		return
	}
	if _, err := qtx.ArchiveIssueWorkflowStatus(r.Context(), db.ArchiveIssueWorkflowStatusParams{
		StatusID: statusID, WorkspaceID: workspaceID, WorkflowID: workflowID,
	}); err != nil {
		writeError(w, http.StatusConflict, "workflow status is no longer archivable")
		return
	}
	workflow, err = qtx.BumpIssueWorkflowRevision(r.Context(), db.BumpIssueWorkflowRevisionParams{
		ID: workflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to version workflow")
		return
	}
	response, err := workflowResponseForRequest(r, qtx, workspaceID, workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload workflow")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow status")
		return
	}
	h.publishIssueWorkflowChanged(uuidToString(workspaceID), member, "workflow_status_archived", workflowID)
	writeJSON(w, http.StatusOK, response)
}

// ReorderIssueWorkflowStatuses replaces the complete active-node order in one
// atomic write. A partial or stale list is rejected before any position moves.
func (h *Handler) ReorderIssueWorkflowStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID, workflowID, member, ok := h.workflowMutationContext(w, r)
	if !ok {
		return
	}
	var req reorderIssueWorkflowStatusesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.StatusIDs) == 0 {
		writeError(w, http.StatusBadRequest, "status_ids must not be empty")
		return
	}
	if req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return
	}
	statusIDs := make([]pgtype.UUID, len(req.StatusIDs))
	seen := make(map[string]struct{}, len(req.StatusIDs))
	for i, raw := range req.StatusIDs {
		if _, duplicate := seen[raw]; duplicate {
			writeError(w, http.StatusBadRequest, "duplicate status_ids")
			return
		}
		seen[raw] = struct{}{}
		id, parseErr := util.ParseUUID(raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow status id")
			return
		}
		statusIDs[i] = id
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder workflow statuses")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	workflow, err := qtx.LockEditableIssueWorkflow(r.Context(), db.LockEditableIssueWorkflowParams{
		WorkflowID: workflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "workflow is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workflow")
		return
	}
	if workflow.Revision != req.ExpectedRevision {
		writeError(w, http.StatusConflict, "workflow revision changed; reload and retry")
		return
	}
	active, err := qtx.ListActiveIssueWorkflowStatuses(r.Context(), db.ListActiveIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflowID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow statuses")
		return
	}
	if len(active) != len(statusIDs) {
		writeError(w, http.StatusConflict, "status_ids must name every active workflow status")
		return
	}
	activeSet := make(map[pgtype.UUID]struct{}, len(active))
	orderChanged := false
	for _, status := range active {
		activeSet[status.ID] = struct{}{}
	}
	for i, id := range statusIDs {
		if _, exists := activeSet[id]; !exists {
			writeError(w, http.StatusConflict, "status_ids contain a status outside this active workflow")
			return
		}
		if active[i].ID != id {
			orderChanged = true
		}
	}
	if orderChanged {
		affected, reorderErr := qtx.ReorderIssueWorkflowStatuses(r.Context(), db.ReorderIssueWorkflowStatusesParams{
			WorkspaceID: workspaceID, WorkflowID: workflowID, StatusIds: statusIDs,
		})
		if reorderErr != nil || affected != int64(len(statusIDs)) {
			writeError(w, http.StatusConflict, "workflow status order changed concurrently")
			return
		}
		workflow, err = qtx.BumpIssueWorkflowRevision(r.Context(), db.BumpIssueWorkflowRevisionParams{
			ID: workflowID, WorkspaceID: workspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to version workflow")
			return
		}
	}
	response, err := workflowResponseForRequest(r, qtx, workspaceID, workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload workflow")
		return
	}
	if orderChanged {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit workflow order")
			return
		}
		h.publishIssueWorkflowChanged(uuidToString(workspaceID), member, "workflow_statuses_reordered", workflowID)
	}
	writeJSON(w, http.StatusOK, response)
}

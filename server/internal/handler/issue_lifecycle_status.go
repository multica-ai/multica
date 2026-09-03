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
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type updateIssueLifecycleStatusRequest struct {
	ExpectedRevision int64                       `json:"expected_revision"`
	Name             *string                     `json:"name"`
	Description      *string                     `json:"description"`
	Color            *string                     `json:"color"`
	Phase            *string                     `json:"phase"`
	EntryPolicy      *issuelifecycle.EntryPolicy `json:"entry_policy"`
}

type reorderIssueLifecycleStatusesRequest struct {
	ExpectedRevision int64    `json:"expected_revision"`
	StatusIDs        []string `json:"status_ids"`
}

func lifecycleOutcome(phase string) (pgtype.Text, bool) {
	switch phase {
	case issuelifecycle.PhaseBacklog, issuelifecycle.PhaseUnstarted, issuelifecycle.PhaseStarted:
		return pgtype.Text{}, true
	case issuelifecycle.PhaseCompleted:
		return pgtype.Text{String: "completed", Valid: true}, true
	case issuelifecycle.PhaseCancelled:
		return pgtype.Text{String: "cancelled", Valid: true}, true
	default:
		return pgtype.Text{}, false
	}
}

func lifecycleProjectID(lifecycle db.IssueLifecycle) pgtype.UUID {
	if lifecycle.ScopeType == "project" {
		return lifecycle.ScopeID
	}
	return pgtype.UUID{}
}

func (h *Handler) lifecycleMutationContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	lifecycleID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "lifecycleId"), "lifecycle id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, db.Member{}, false
	}
	return workspaceUUID, lifecycleID, member, true
}

func (h *Handler) validateEntryPolicyReferences(w http.ResponseWriter, r *http.Request, workspaceID string, policy issuelifecycle.EntryPolicy) bool {
	validate := func(principal issuelifecycle.EntryPolicyPrincipal, executor bool) bool {
		if principal.Type == issuelifecycle.AssigneeKeep || principal.Type == issuelifecycle.ExecutorNone {
			return true
		}
		id, ok := parseUUIDOrBadRequest(w, principal.ID, "entry policy principal id")
		if !ok {
			return false
		}
		principalType := principal.Type
		if principalType == issuelifecycle.AssigneeHuman {
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

func (h *Handler) publishIssueLifecycleChanged(workspaceID string, member db.Member, action string, lifecycleID pgtype.UUID) {
	h.publish(protocol.EventIssueStatusChanged, workspaceID, "member", uuidToString(member.UserID), map[string]any{
		"action": action, "lifecycle_id": uuidToString(lifecycleID),
	})
}

func lifecycleResponseForRequest(r *http.Request, q *db.Queries, workspaceID pgtype.UUID, lifecycle db.IssueLifecycle) (issueLifecycleResponse, error) {
	statuses, err := q.ListIssueLifecycleStatuses(r.Context(), db.ListIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycle.ID, IncludeArchived: true,
	})
	if err != nil {
		return issueLifecycleResponse{}, err
	}
	return buildIssueLifecycleResponse(lifecycle, statuses, lifecycleProjectID(lifecycle)), nil
}

// UpdateIssueLifecycleStatus edits one active stable status node. The
// lifecycle row lock serializes definition revisions; entry_policy_revision
// advances independently and only when the normalized policy changes.
func (h *Handler) UpdateIssueLifecycleStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, lifecycleID, member, ok := h.lifecycleMutationContext(w, r)
	if !ok {
		return
	}
	statusID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "statusId"), "lifecycle status id")
	if !ok {
		return
	}
	var req updateIssueLifecycleStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return
	}
	if req.Name == nil && req.Description == nil && req.Color == nil && req.Phase == nil && req.EntryPolicy == nil {
		writeError(w, http.StatusBadRequest, "at least one lifecycle status field is required")
		return
	}

	var requestedPolicy []byte
	var normalizedPolicy issuelifecycle.EntryPolicy
	if req.EntryPolicy != nil {
		var err error
		requestedPolicy, normalizedPolicy, err = issuelifecycle.EncodeEntryPolicy(*req.EntryPolicy)
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
		writeError(w, http.StatusInternalServerError, "failed to update lifecycle status")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	lifecycle, err := qtx.LockEditableIssueLifecycle(r.Context(), db.LockEditableIssueLifecycleParams{
		LifecycleID: lifecycleID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "lifecycle is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock lifecycle")
		return
	}
	if lifecycle.Revision != req.ExpectedRevision {
		writeError(w, http.StatusConflict, "lifecycle revision changed; reload and retry")
		return
	}
	current, err := qtx.GetIssueLifecycleStatusByID(r.Context(), db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycleID, ID: statusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "lifecycle status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle status")
		return
	}
	if current.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived lifecycle statuses cannot be modified")
		return
	}
	active, err := qtx.ListActiveIssueLifecycleStatuses(r.Context(), db.ListActiveIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycleID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle statuses")
		return
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
			writeError(w, http.StatusConflict, "an active lifecycle status with this name already exists")
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
	outcome, validPhase := lifecycleOutcome(phase)
	if !validPhase {
		writeError(w, http.StatusBadRequest, "phase must be backlog, unstarted, started, completed, or cancelled")
		return
	}

	entryPolicy := current.EntryPolicy
	policyChanged := false
	if req.EntryPolicy != nil {
		storedPolicy, decodeErr := issuelifecycle.DecodeEntryPolicy(current.EntryPolicy)
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
		response, listErr := lifecycleResponseForRequest(r, qtx, workspaceID, lifecycle)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload lifecycle")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	if _, err = qtx.UpdateIssueLifecycleStatusDefinition(r.Context(), db.UpdateIssueLifecycleStatusDefinitionParams{
		Name: name, Description: description, Color: color, Position: position,
		Phase: phase, Outcome: outcome, EntryPolicy: entryPolicy,
		BumpEntryPolicyRevision: policyChanged, StatusID: statusID,
		WorkspaceID: workspaceID, LifecycleID: lifecycleID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "lifecycle status is no longer editable")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a lifecycle status with this name already exists")
			return
		}
		slog.Warn("update lifecycle status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update lifecycle status")
		return
	}
	lifecycle, err = qtx.BumpIssueLifecycleRevision(r.Context(), db.BumpIssueLifecycleRevisionParams{
		ID: lifecycleID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to version lifecycle")
		return
	}
	response, err := lifecycleResponseForRequest(r, qtx, workspaceID, lifecycle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload lifecycle")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit lifecycle status")
		return
	}
	h.publishIssueLifecycleChanged(uuidToString(workspaceID), member, "lifecycle_status_updated", lifecycleID)
	writeJSON(w, http.StatusOK, response)
}

// ArchiveIssueLifecycleStatus retires a node from future transitions while
// preserving existing issue bindings and transition history.
func (h *Handler) ArchiveIssueLifecycleStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, lifecycleID, member, ok := h.lifecycleMutationContext(w, r)
	if !ok {
		return
	}
	expectedRevision, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("expected_revision")), 10, 64)
	if err != nil || expectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return
	}
	statusID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "statusId"), "lifecycle status id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive lifecycle status")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	lifecycle, err := qtx.LockEditableIssueLifecycle(r.Context(), db.LockEditableIssueLifecycleParams{
		LifecycleID: lifecycleID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "lifecycle is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock lifecycle")
		return
	}
	if lifecycle.Revision != expectedRevision {
		writeError(w, http.StatusConflict, "lifecycle revision changed; reload and retry")
		return
	}
	current, err := qtx.GetIssueLifecycleStatusByID(r.Context(), db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycleID, ID: statusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "lifecycle status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle status")
		return
	}
	if current.ArchivedAt.Valid {
		response, listErr := lifecycleResponseForRequest(r, qtx, workspaceID, lifecycle)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload lifecycle")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	active, err := qtx.ListActiveIssueLifecycleStatuses(r.Context(), db.ListActiveIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycleID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle statuses")
		return
	}
	if len(active) <= 1 {
		writeError(w, http.StatusConflict, "a lifecycle must keep at least one active status")
		return
	}
	if _, err := qtx.ArchiveIssueLifecycleStatus(r.Context(), db.ArchiveIssueLifecycleStatusParams{
		StatusID: statusID, WorkspaceID: workspaceID, LifecycleID: lifecycleID,
	}); err != nil {
		writeError(w, http.StatusConflict, "lifecycle status is no longer archivable")
		return
	}
	lifecycle, err = qtx.BumpIssueLifecycleRevision(r.Context(), db.BumpIssueLifecycleRevisionParams{
		ID: lifecycleID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to version lifecycle")
		return
	}
	response, err := lifecycleResponseForRequest(r, qtx, workspaceID, lifecycle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload lifecycle")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit lifecycle status")
		return
	}
	h.publishIssueLifecycleChanged(uuidToString(workspaceID), member, "lifecycle_status_archived", lifecycleID)
	writeJSON(w, http.StatusOK, response)
}

// ReorderIssueLifecycleStatuses replaces the complete active-node order in one
// atomic write. A partial or stale list is rejected before any position moves.
func (h *Handler) ReorderIssueLifecycleStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID, lifecycleID, member, ok := h.lifecycleMutationContext(w, r)
	if !ok {
		return
	}
	var req reorderIssueLifecycleStatusesRequest
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
			writeError(w, http.StatusBadRequest, "invalid lifecycle status id")
			return
		}
		statusIDs[i] = id
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder lifecycle statuses")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	lifecycle, err := qtx.LockEditableIssueLifecycle(r.Context(), db.LockEditableIssueLifecycleParams{
		LifecycleID: lifecycleID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "lifecycle is not currently editable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock lifecycle")
		return
	}
	if lifecycle.Revision != req.ExpectedRevision {
		writeError(w, http.StatusConflict, "lifecycle revision changed; reload and retry")
		return
	}
	active, err := qtx.ListActiveIssueLifecycleStatuses(r.Context(), db.ListActiveIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycleID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lifecycle statuses")
		return
	}
	if len(active) != len(statusIDs) {
		writeError(w, http.StatusConflict, "status_ids must name every active lifecycle status")
		return
	}
	activeSet := make(map[pgtype.UUID]struct{}, len(active))
	orderChanged := false
	for _, status := range active {
		activeSet[status.ID] = struct{}{}
	}
	for i, id := range statusIDs {
		if _, exists := activeSet[id]; !exists {
			writeError(w, http.StatusConflict, "status_ids contain a status outside this active lifecycle")
			return
		}
		if active[i].ID != id {
			orderChanged = true
		}
	}
	if orderChanged {
		affected, reorderErr := qtx.ReorderIssueLifecycleStatuses(r.Context(), db.ReorderIssueLifecycleStatusesParams{
			WorkspaceID: workspaceID, LifecycleID: lifecycleID, StatusIds: statusIDs,
		})
		if reorderErr != nil || affected != int64(len(statusIDs)) {
			writeError(w, http.StatusConflict, "lifecycle status order changed concurrently")
			return
		}
		lifecycle, err = qtx.BumpIssueLifecycleRevision(r.Context(), db.BumpIssueLifecycleRevisionParams{
			ID: lifecycleID, WorkspaceID: workspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to version lifecycle")
			return
		}
	}
	response, err := lifecycleResponseForRequest(r, qtx, workspaceID, lifecycle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload lifecycle")
		return
	}
	if orderChanged {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit lifecycle order")
			return
		}
		h.publishIssueLifecycleChanged(uuidToString(workspaceID), member, "lifecycle_statuses_reordered", lifecycleID)
	}
	writeJSON(w, http.StatusOK, response)
}

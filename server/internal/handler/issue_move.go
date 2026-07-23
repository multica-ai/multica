package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

var issueMoveFields = map[string]struct{}{
	"status":          {},
	"assignee_type":   {},
	"assignee_id":     {},
	"parent_issue_id": {},
	"project_id":      {},
	"before_id":       {},
	"after_id":        {},
}

// MoveIssue accepts relative neighbors instead of a client-authored canonical
// position. It resolves both anchors inside the issue's workspace, derives the
// position server-side, then delegates the actual write to UpdateIssue so all
// existing validation, realtime, task-trigger and parent-notification behavior
// stays on one path. The legacy PUT endpoint remains unchanged for released
// clients.
func (h *Handler) MoveIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for field := range fields {
		if _, ok := issueMoveFields[field]; !ok {
			writeError(w, http.StatusBadRequest, "unsupported move field: "+field)
			return
		}
	}
	if _, ok := fields["before_id"]; !ok {
		writeError(w, http.StatusBadRequest, "before_id is required")
		return
	}
	if _, ok := fields["after_id"]; !ok {
		writeError(w, http.StatusBadRequest, "after_id is required")
		return
	}
	if rawProjectID, touched := fields["project_id"]; touched && !rawJSONNull(rawProjectID) {
		projectID, valid := decodeIssueMoveAnchor(w, rawProjectID, "project_id")
		if !valid {
			return
		}
		var exists bool
		err := h.DB.QueryRow(r.Context(), `
			SELECT EXISTS (
				SELECT 1
				FROM project
				WHERE workspace_id = $1 AND id = $2
			)
		`, current.WorkspaceID, *projectID).Scan(&exists)
		if err != nil {
			writeIssueTableQueryFailure(w, r, "failed to validate move project")
			return
		}
		if !exists {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
	}

	beforeID, ok := decodeIssueMoveAnchor(w, fields["before_id"], "before_id")
	if !ok {
		return
	}
	afterID, ok := decodeIssueMoveAnchor(w, fields["after_id"], "after_id")
	if !ok {
		return
	}
	if beforeID != nil && *beforeID == current.ID {
		writeError(w, http.StatusBadRequest, "before_id cannot be the moved issue")
		return
	}
	if afterID != nil && *afterID == current.ID {
		writeError(w, http.StatusBadRequest, "after_id cannot be the moved issue")
		return
	}
	if beforeID != nil && afterID != nil && *beforeID == *afterID {
		writeError(w, http.StatusBadRequest, "move anchors must be distinct")
		return
	}

	// V1 intentionally keeps the canonical write on UpdateIssue so released
	// clients and the new move endpoint share validation, realtime and task
	// side effects. That means anchor resolution and the write are not one
	// transaction: stale/out-of-order anchors fail closed with 409 where
	// detectable, and an exhausted float gap also returns 409 instead of
	// silently renumbering neighboring issues. A future rebalance must move
	// every position writer (including the legacy PUT path) behind one
	// transactional ordering boundary.
	beforePosition, ok := h.issueMoveAnchorPosition(w, r, current.WorkspaceID, beforeID)
	if !ok {
		return
	}
	afterPosition, ok := h.issueMoveAnchorPosition(w, r, current.WorkspaceID, afterID)
	if !ok {
		return
	}
	position, err := issueMovePosition(current.Position, beforePosition, afterPosition)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	delete(fields, "before_id")
	delete(fields, "after_id")
	fields["position"], _ = json.Marshal(position)
	updateBody, err := json.Marshal(fields)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode move")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(updateBody))
	r.ContentLength = int64(len(updateBody))
	h.UpdateIssue(w, r)
}

func decodeIssueMoveAnchor(w http.ResponseWriter, raw json.RawMessage, field string) (*pgtype.UUID, bool) {
	if rawJSONNull(raw) {
		return nil, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		writeError(w, http.StatusBadRequest, field+" must be a UUID or null")
		return nil, false
	}
	id, err := util.ParseUUID(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return nil, false
	}
	return &id, true
}

func rawJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (h *Handler) issueMoveAnchorPosition(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID pgtype.UUID,
	id *pgtype.UUID,
) (*float64, bool) {
	if id == nil {
		return nil, true
	}
	var position float64
	err := h.DB.QueryRow(r.Context(), `
		SELECT position
		FROM issue
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, *id).Scan(&position)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "move anchor not found in this workspace")
		} else {
			writeIssueTableQueryFailure(w, r, "failed to resolve move anchor")
		}
		return nil, false
	}
	return &position, true
}

func issueMovePosition(current float64, before, after *float64) (float64, error) {
	switch {
	case before != nil && after != nil:
		if !(*before < *after) {
			return 0, errors.New("move anchors are stale or out of order")
		}
		position := *before + (*after-*before)/2
		if !(position > *before && position < *after) ||
			math.IsInf(position, 0) || math.IsNaN(position) {
			return 0, errors.New("move anchors are too close; refresh and retry")
		}
		return position, nil
	case before != nil:
		position := *before + 1
		if math.IsInf(position, 0) || math.IsNaN(position) {
			return 0, errors.New("move position is out of range")
		}
		return position, nil
	case after != nil:
		position := *after - 1
		if math.IsInf(position, 0) || math.IsNaN(position) {
			return 0, errors.New("move position is out of range")
		}
		return position, nil
	default:
		return current, nil
	}
}

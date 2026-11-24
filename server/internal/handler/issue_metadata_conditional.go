package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Resolve match, capacity and no-op under the same owner lock as the mutation.
// A later writer must not turn a rejected precondition into a successful no-op.
func (h *Handler) mutateConditionalIssueMetadata(w http.ResponseWriter, r *http.Request, issue db.Issue, userID, key string, value json.RawMessage, expected int64) {
	started := time.Now()
	operation, outcome := "set", "error"
	if value == nil {
		operation = "delete"
	}
	defer func() {
		h.recordIssueMetadataMutation(r, operation, outcome, uuidToString(issue.ID), key, time.Since(started))
	}()

	conflict := errors.New("metadata revision conflict")
	capacity := errors.New("metadata key capacity")
	var currentRevision int64
	changed := false
	result, err := wakeupWrite(h, r, func(q *db.Queries) (db.GetIssueMetadataInWorkspaceRow, error) {
		var zero db.GetIssueMetadataInWorkspaceRow
		if _, err := q.LockIssueForWrite(r.Context(), db.LockIssueForWriteParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
			return zero, err
		}
		current, err := q.GetIssueMetadataInWorkspace(r.Context(), db.GetIssueMetadataInWorkspaceParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return zero, err
		}
		currentRevision = current.Revision
		if current.Revision != expected {
			return zero, conflict
		}
		condition := pgtype.Int8{Int64: expected, Valid: true}
		if value != nil {
			existing := parseIssueMetadata(current.Metadata)
			if _, present := existing[key]; !present && len(existing) >= maxIssueMetadataKeys {
				return zero, capacity
			}
			updated, err := q.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: key, Value: value, ExpectedRevision: condition})
			if errors.Is(err, pgx.ErrNoRows) {
				return current, nil
			}
			if err != nil {
				return zero, err
			}
			changed = true
			return db.GetIssueMetadataInWorkspaceRow{Metadata: updated.Metadata, Revision: updated.Revision}, nil
		}
		updated, err := q.DeleteIssueMetadataKey(r.Context(), db.DeleteIssueMetadataKeyParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: key, ExpectedRevision: condition})
		if errors.Is(err, pgx.ErrNoRows) {
			return current, nil
		}
		if err != nil {
			return zero, err
		}
		changed = true
		return db.GetIssueMetadataInWorkspaceRow{Metadata: updated.Metadata, Revision: updated.Revision}, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, conflict):
			writeRevisionConflict(w, "issue", issue.ID, expected, currentRevision)
		case errors.Is(err, capacity):
			writeError(w, http.StatusBadRequest, fmt.Sprintf("metadata cannot exceed %d keys", maxIssueMetadataKeys))
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "issue not found")
		case isCheckViolation(err):
			writeError(w, http.StatusBadRequest, "metadata exceeds the 8KB size limit")
		default:
			slog.Warn("conditional metadata mutation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to mutate metadata key")
		}
		return
	}
	outcome = "noop"
	if changed {
		outcome = "changed"
	}
	metadata := parseIssueMetadata(result.Metadata)
	if changed {
		workspaceID := uuidToString(issue.WorkspaceID)
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueMetadataChanged, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "metadata": metadata, "issue_revision": result.Revision})
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata, "issue_revision": result.Revision})
}

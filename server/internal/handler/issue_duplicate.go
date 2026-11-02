package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Duplicate marks (MUL-7349). A duplicate is an ordinary cancelled issue that
// remembers which issue it duplicates: there is no duplicate status. Setting
// the mark goes through UpdateIssue as a status write, and any status change
// away from cancelled clears it in SQL (UpdateIssue / UpdateIssueStatus).
//
// The relation stays one level deep in both directions: a target must not be
// a duplicate itself, and an issue that others are marked as duplicates of
// cannot be marked. Nothing is collapsed or re-pointed automatically.
//
// The column and the code that maintains it shipped one release earlier, so a
// rollback lands on a server that still clears the pointer when an issue is
// reopened or its original deleted. Reads below additionally require the
// duplicate to be cancelled and its original to exist, which keeps a rollback
// that goes back further than that release readable rather than wrong.

var (
	errDuplicateTargetNotFound    = errors.New("duplicate target not found in this workspace")
	errDuplicateTargetIsDuplicate = errors.New("duplicate target is itself a duplicate")
	errIssueHasDuplicates         = errors.New("issue has duplicates")
)

// parseDuplicateMark reads duplicate_of_issue_id from an update request. When
// present it turns the request into a status write to cancelled, so the mark
// shares the status resolution, status_changed event and stage-barrier
// handling of cancelling by hand. It returns an invalid UUID when the request
// does not mark, and false after writing an error response.
func parseDuplicateMark(w http.ResponseWriter, req *UpdateIssueRequest, rawFields map[string]json.RawMessage, issueID pgtype.UUID) (pgtype.UUID, bool) {
	if _, present := rawFields["duplicate_of_issue_id"]; !present {
		return pgtype.UUID{}, true
	}
	if req.DuplicateOfIssueID == nil {
		writeError(w, http.StatusBadRequest, "duplicate_of_issue_id cannot be null; change the status to remove a duplicate mark")
		return pgtype.UUID{}, false
	}
	targetID, ok := parseUUIDOrBadRequest(w, *req.DuplicateOfIssueID, "duplicate_of_issue_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if targetID == issueID {
		writeError(w, http.StatusBadRequest, "an issue cannot be a duplicate of itself")
		return pgtype.UUID{}, false
	}
	if req.Status != nil && *req.Status != issuestatus.Cancelled {
		writeError(w, http.StatusBadRequest, "a duplicate is cancelled; status must be cancelled or omitted")
		return pgtype.UUID{}, false
	}
	// The description/attachment path locks attachments before the issue row,
	// while a mark locks two issue rows first. Keeping them apart keeps each
	// path's lock order fixed.
	if req.Description != nil || req.TitleBase != nil || req.DescriptionBase != nil || len(req.AttachmentIDs) > 0 {
		writeError(w, http.StatusBadRequest, "duplicate_of_issue_id cannot be combined with description or attachment changes")
		return pgtype.UUID{}, false
	}
	cancelled := issuestatus.Cancelled
	req.Status = &cancelled
	return targetID, true
}

// lockAndCheckDuplicateMark validates a mark inside the update transaction.
// Both issues are locked first, so marks that share an issue run one after
// the other and each validates what the previous one wrote. Deleting the
// target also locks it, so a mark never commits a pointer to a deleted issue.
func lockAndCheckDuplicateMark(ctx context.Context, q *db.Queries, workspaceID, issueID, targetID pgtype.UUID) error {
	rows, err := q.LockIssuesForDuplicateMark(ctx, db.LockIssuesForDuplicateMarkParams{
		WorkspaceID: workspaceID,
		IssueIds:    []pgtype.UUID{issueID, targetID},
	})
	if err != nil {
		return fmt.Errorf("lock issues for duplicate mark: %w", err)
	}
	targetFound := false
	for _, row := range rows {
		if row.ID != targetID {
			continue
		}
		targetFound = true
		if row.IsDuplicate {
			return errDuplicateTargetIsDuplicate
		}
	}
	if !targetFound {
		return errDuplicateTargetNotFound
	}
	hasDuplicates, err := q.IssueHasDuplicates(ctx, db.IssueHasDuplicatesParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		return fmt.Errorf("check issue duplicates: %w", err)
	}
	if hasDuplicates {
		return errIssueHasDuplicates
	}
	return nil
}

func writeDuplicateMarkError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errDuplicateTargetNotFound):
		writeError(w, http.StatusBadRequest, "duplicate target not found in this workspace")
	case errors.Is(err, errDuplicateTargetIsDuplicate):
		writeErrorCode(w, http.StatusConflict, "duplicate_target_is_duplicate",
			"the target issue is itself a duplicate; mark this issue as a duplicate of its original instead")
	case errors.Is(err, errIssueHasDuplicates):
		writeErrorCode(w, http.StatusConflict, "issue_has_duplicates",
			"other issues are marked as duplicates of this issue; remove those marks first")
	default:
		return false
	}
	return true
}

// liveDuplicateMark renders one end of a mark change for an issue:updated
// payload: the pointer only while it counts, i.e. the issue is cancelled. A
// pointer an older server left on a reopened issue is no mark, so a write
// that clears it is not a mark being removed.
func liveDuplicateMark(status string, id pgtype.UUID) *string {
	return uuidToPtr(duplicateOfPointer(status, id))
}

// ListIssueDuplicates returns both sides of an issue's duplicate relation: the
// original it duplicates, if any, and the issues marked as its duplicates.
func (h *Handler) ListIssueDuplicates(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	fill := h.newStatusCategoryFiller(r.Context(), issue.WorkspaceID)

	var duplicateOf *IssueResponse
	if issue.Status == issuestatus.Cancelled && issue.DuplicateOfIssueID.Valid {
		original, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issue.DuplicateOfIssueID,
			WorkspaceID: issue.WorkspaceID,
		})
		switch {
		case err == nil:
			resp := issueToResponse(original, prefix)
			fill(&resp)
			duplicateOf = &resp
		case !isNotFound(err):
			slog.Warn("load duplicate original failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to load duplicate original")
			return
		}
	}

	rows, err := h.Queries.ListIssueDuplicates(r.Context(), db.ListIssueDuplicatesParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list issue duplicates failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list duplicates")
		return
	}
	duplicates := make([]IssueResponse, 0, len(rows))
	for _, row := range rows {
		resp := issueToResponse(row, prefix)
		fill(&resp)
		duplicates = append(duplicates, resp)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"duplicate_of": duplicateOf,
		"duplicates":   duplicates,
	})
}

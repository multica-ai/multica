package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const codeMRSnapshotBodyLimit = 64 << 10

func parseOptionalCodeMRTime(raw string) (pgtype.Timestamptz, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pgtype.Timestamptz{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, nil
}

func validateCodeMRSnapshot(result protocol.CodeMRSnapshotResult) error {
	if len(strings.TrimSpace(result.Title)) == 0 || len(result.Title) > 1000 {
		return errors.New("invalid title")
	}
	switch result.State {
	case "open", "closed", "merged", "draft":
	default:
		return errors.New("invalid state")
	}
	if result.CommentCount < 0 || result.UnresolvedCommentCount < 0 || result.UnresolvedCommentCount > result.CommentCount || result.CommentCount > 1000000 {
		return errors.New("invalid comment counts")
	}
	if _, err := parseOptionalCodeMRTime(result.CreatedAt); err != nil {
		return errors.New("invalid created_at")
	}
	if _, err := parseOptionalCodeMRTime(result.UpdatedAt); err != nil {
		return errors.New("invalid updated_at")
	}
	return nil
}

// ReportCodeMRSnapshot accepts normalized a1 output from an authenticated
// daemon runtime. The runtime and MR row must belong to the same workspace.
func (h *Handler) ReportCodeMRSnapshot(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	prID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "externalPullRequestId"), "external_pull_request_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetExternalPullRequestByID(r.Context(), db.GetExternalPullRequestByIDParams{
		ID: prID, WorkspaceID: runtime.WorkspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "external pull request not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load external pull request")
		return
	}

	var result protocol.CodeMRSnapshotResult
	decoder := json.NewDecoder(io.LimitReader(r.Body, codeMRSnapshotBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Code MR snapshot")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid Code MR snapshot")
		return
	}
	if result.Error != "" {
		safe := strings.TrimSpace(redact.Text(result.Error))
		safe = util.TruncateUTF8(safe, 1024)
		updated, updateErr := h.Queries.FailExternalPullRequestSync(r.Context(), db.FailExternalPullRequestSyncParams{
			ID: prID, SyncError: pgtype.Text{String: safe, Valid: safe != ""}, WorkspaceID: runtime.WorkspaceID,
		})
		if updateErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to record Code MR sync failure")
			return
		}
		h.publishCodeMRSnapshot(updated)
		writeJSON(w, http.StatusOK, externalPullRequestToResponse(updated))
		return
	}
	if err := validateCodeMRSnapshot(result); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Code MR snapshot")
		return
	}
	createdAt, _ := parseOptionalCodeMRTime(result.CreatedAt)
	updatedAt, _ := parseOptionalCodeMRTime(result.UpdatedAt)
	ready := pgtype.Bool{}
	if result.ReadyToMerge != nil {
		ready = pgtype.Bool{Bool: *result.ReadyToMerge, Valid: true}
	}
	updated, err := h.Queries.UpdateExternalPullRequestSync(r.Context(), db.UpdateExternalPullRequestSyncParams{
		ID:                     prID,
		Title:                  result.Title,
		State:                  result.State,
		SourceBranch:           pgtype.Text{String: result.SourceBranch, Valid: result.SourceBranch != ""},
		TargetBranch:           pgtype.Text{String: result.TargetBranch, Valid: result.TargetBranch != ""},
		AuthorLogin:            pgtype.Text{String: result.AuthorLogin, Valid: result.AuthorLogin != ""},
		PrCreatedAt:            createdAt,
		PrUpdatedAt:            updatedAt,
		ReadyToMerge:           ready,
		CommentCount:           result.CommentCount,
		UnresolvedCommentCount: result.UnresolvedCommentCount,
		WorkspaceID:            runtime.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update Code MR snapshot")
		return
	}
	h.publishCodeMRSnapshot(updated)
	writeJSON(w, http.StatusOK, externalPullRequestToResponse(updated))
}

func (h *Handler) publishCodeMRSnapshot(row db.ExternalPullRequest) {
	h.publish(protocol.EventPullRequestUpdated, uuidToString(row.WorkspaceID), "system", "", map[string]any{
		"pull_request":     externalPullRequestToResponse(row),
		"linked_issue_ids": []string{uuidToString(row.IssueID)},
	})
}

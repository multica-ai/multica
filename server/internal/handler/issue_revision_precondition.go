package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func parseIssueExpectedRevision(w http.ResponseWriter, r *http.Request) (pgtype.Int8, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query parameters")
		return pgtype.Int8{}, false
	}
	values, present := query["expected_revision"]
	if !present {
		return pgtype.Int8{}, true
	}
	if len(values) != 1 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a single positive integer")
		return pgtype.Int8{}, false
	}
	revision, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || revision < 1 {
		writeError(w, http.StatusBadRequest, "expected_revision must be a positive integer")
		return pgtype.Int8{}, false
	}
	return pgtype.Int8{Int64: revision, Valid: true}, true
}

// RequireIssueExpectedRevision keeps the conditional mutation routes from
// accepting an accidental unconditional write.
func RequireIssueExpectedRevision(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected, ok := parseIssueExpectedRevision(w, r)
		if !ok {
			return
		}
		if !expected.Valid {
			writeError(w, http.StatusBadRequest, "expected_revision is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) writeCurrentIssueRevisionConflict(w http.ResponseWriter, r *http.Request, issue db.Issue, expected int64) {
	current, err := h.Queries.GetIssueMetadataInWorkspace(r.Context(), db.GetIssueMetadataInWorkspaceParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err != nil {
		slog.Warn("failed to read current issue revision", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read current issue revision")
		return
	}
	writeRevisionConflict(w, "issue", issue.ID, expected, current.Revision)
}

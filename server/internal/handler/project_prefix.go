package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validateProjectIssuePrefix normalizes a client-supplied project prefix and
// rejects the two ambiguities the workspace rule forbids: a prefix equal to the
// workspace's own prefix, and a prefix already used by a sibling project.
// An empty value means "no project scope" and always passes.
func (h *Handler) validateProjectIssuePrefix(w http.ResponseWriter, r *http.Request, q *db.Queries, workspaceID, projectID pgtype.UUID, raw string) (string, bool) {
	prefix, ok := normalizeIssuePrefix(raw)
	if !ok {
		writeError(w, http.StatusBadRequest, issuePrefixFormatError)
		return "", false
	}
	if prefix == "" {
		return "", true
	}
	if wsPrefix := h.getIssuePrefix(r.Context(), workspaceID); strings.EqualFold(prefix, wsPrefix) {
		writeError(w, http.StatusConflict, "issue prefix "+prefix+" is already used by the workspace; choose another one")
		return "", false
	}
	existing, err := q.GetProjectByIssuePrefix(r.Context(), db.GetProjectByIssuePrefixParams{
		WorkspaceID: workspaceID,
		IssuePrefix: prefix,
	})
	switch {
	case err == nil && existing.ID != projectID:
		writeError(w, http.StatusConflict, "issue prefix "+prefix+" is already used by project "+existing.Title)
		return "", false
	case err != nil && !isNotFound(err):
		slog.Error("project prefix lookup failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to validate issue prefix")
		return "", false
	}
	return prefix, true
}

// renumberProjectIssues assigns the project's issues fresh numbers from a reset
// project counter, ordered by issue id so the result is deterministic. Callers
// run it in the transaction that changes the prefix; the project row lock taken
// before it serializes concurrent creates in the same project.
func (h *Handler) renumberProjectIssues(ctx context.Context, q *db.Queries, projectID pgtype.UUID) error {
	issueIDs, err := q.ListProjectIssueIDs(ctx, projectID)
	if err != nil {
		return err
	}
	if err := q.SetProjectIssueCounter(ctx, db.SetProjectIssueCounterParams{ID: projectID}); err != nil {
		return err
	}
	for _, issueID := range issueIDs {
		number, err := q.IncrementProjectIssueCounter(ctx, projectID)
		if err != nil {
			return err
		}
		if err := q.SetIssueNumber(ctx, db.SetIssueNumberParams{ID: issueID, Number: int32(number)}); err != nil {
			return err
		}
	}
	return nil
}

// renumberIssuesToWorkspace moves issues back into the workspace numbering
// scope, used when a prefixed project is deleted and its issues fall back to
// the workspace prefix (issue.project_id becomes NULL).
func (h *Handler) renumberIssuesToWorkspace(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, issueIDs []pgtype.UUID) error {
	for _, issueID := range issueIDs {
		number, err := q.IncrementIssueCounter(ctx, workspaceID)
		if err != nil {
			return err
		}
		if err := q.SetIssueNumber(ctx, db.SetIssueNumberParams{ID: issueID, Number: number}); err != nil {
			return err
		}
	}
	return nil
}

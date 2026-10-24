package handler

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

type issueDependencyResponse struct {
	IssueID          string `json:"issue_id"`
	DependsOnIssueID string `json:"depends_on_issue_id"`
	Type             string `json:"type"`
}

// ListIssueDependencies returns only relationships whose two issues belong to
// the selected workspace. The workspace membership gate also runs in the router.
func (h *Handler) ListIssueDependencies(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT d.issue_id, d.depends_on_issue_id, d.type
		FROM issue_dependency d
		JOIN issue source ON source.id = d.issue_id
		JOIN issue target ON target.id = d.depends_on_issue_id
		WHERE source.workspace_id = $1 AND target.workspace_id = $1
		ORDER BY d.issue_id, d.depends_on_issue_id, d.id
	`, wsUUID)
	if err != nil {
		slog.Warn("ListIssueDependencies failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue dependencies")
		return
	}
	defer rows.Close()
	dependencies := make([]issueDependencyResponse, 0)
	for rows.Next() {
		var source, target pgtype.UUID
		var kind string
		if err := rows.Scan(&source, &target, &kind); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list issue dependencies")
			return
		}
		dependencies = append(dependencies, issueDependencyResponse{
			IssueID: util.UUIDToString(source), DependsOnIssueID: util.UUIDToString(target), Type: kind,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue dependencies")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dependencies": dependencies, "complete": true})
}

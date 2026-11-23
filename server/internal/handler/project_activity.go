package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	projectActivityDefaultLimit = 30
	projectActivityMaxLimit     = 100
)

// projectActivityActions are the issue activities a project feed shows: the
// changes a person following the project acts on. Description autosaves, run
// bookkeeping and squad evaluations stay on the issue's own timeline.
var projectActivityActions = []string{
	"created",
	"status_changed",
	"assignee_changed",
	"priority_changed",
	"start_date_changed",
	"due_date_changed",
	"title_changed",
	"duplicate_marked",
}

// ProjectActivityEntry is one activity on an issue in the project, with the
// issue it happened on.
type ProjectActivityEntry struct {
	ID              string          `json:"id"`
	IssueID         string          `json:"issue_id"`
	IssueIdentifier string          `json:"issue_identifier"`
	IssueTitle      string          `json:"issue_title"`
	IssueStatus     string          `json:"issue_status"`
	ActorType       string          `json:"actor_type"`
	ActorID         string          `json:"actor_id"`
	Action          string          `json:"action"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       string          `json:"created_at"`
}

type ProjectActivityResponse struct {
	Entries []ProjectActivityEntry `json:"entries"`
	// NextCursor reads the next older page through ?before=; null on the
	// last page.
	NextCursor *string `json:"next_cursor"`
}

// ListProjectActivity returns the newest activity across a project's issues,
// newest first. (MUL-8879)
func (h *Handler) ListProjectActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !h.requireProjectVisible(w, r, workspaceID, project.ID, http.StatusNotFound, "project not found") {
		return
	}

	limit := projectActivityDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, projectActivityMaxLimit)
	}
	params := db.ListProjectActivityParams{
		WorkspaceID: wsUUID,
		ProjectID:   project.ID,
		Actions:     projectActivityActions,
		RowLimit:    int32(limit + 1),
	}
	if before := strings.TrimSpace(r.URL.Query().Get("before")); before != "" {
		params.BeforeAt, params.BeforeID = parseTranscriptCursor(before)
		if !params.BeforeAt.Valid || !params.BeforeID.Valid {
			writeError(w, http.StatusBadRequest, "invalid before cursor")
			return
		}
	}

	rows, err := h.Queries.ListProjectActivity(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project activity")
		return
	}

	resp := ProjectActivityResponse{Entries: make([]ProjectActivityEntry, 0, min(len(rows), limit))}
	if len(rows) > limit {
		last := rows[limit-1]
		cursor := transcriptCursor(last.CreatedAt.Time, last.ID)
		resp.NextCursor = &cursor
		rows = rows[:limit]
	}
	prefix := h.getIssuePrefix(ctx, wsUUID)
	for _, row := range rows {
		details := json.RawMessage(row.Details)
		if len(details) == 0 {
			details = json.RawMessage("{}")
		}
		resp.Entries = append(resp.Entries, ProjectActivityEntry{
			ID:              uuidToString(row.ID),
			IssueID:         uuidToString(row.IssueID),
			IssueIdentifier: prefix + "-" + strconv.Itoa(int(row.IssueNumber)),
			IssueTitle:      row.IssueTitle,
			IssueStatus:     row.IssueStatus,
			ActorType:       row.ActorType.String,
			ActorID:         uuidToString(row.ActorID),
			Action:          row.Action,
			Details:         details,
			CreatedAt:       timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

type CodeGraphAssignedProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *Handler) codeGraphProjectSummaries(r *http.Request, workspaceID pgtype.UUID) (map[string][]CodeGraphAssignedProject, error) {
	rows, err := h.DB.Query(r.Context(), `SELECT b.repository_id::text,p.id::text,p.title
 FROM project_code_graph_repository b JOIN project p ON p.id=b.project_id
 JOIN code_graph_repository g ON g.id=b.repository_id AND g.workspace_id=p.workspace_id
 WHERE p.workspace_id=$1 ORDER BY p.title,p.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]CodeGraphAssignedProject)
	for rows.Next() {
		var repo string
		var p CodeGraphAssignedProject
		if err := rows.Scan(&repo, &p.ID, &p.Name); err != nil {
			return nil, err
		}
		result[repo] = append(result[repo], p)
	}
	return result, rows.Err()
}

func (h *Handler) UpdateCodeGraphWorkspaceScope(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	repo, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	var body struct {
		Shared *bool `json:"is_workspace_shared"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Shared == nil {
		writeError(w, 400, "is_workspace_shared is required")
		return
	}
	tag, err := h.DB.Exec(r.Context(), `UPDATE code_graph_repository SET is_workspace_shared=$3,updated_at=now() WHERE id=$1 AND workspace_id=$2`, repo, parseUUID(ws), *body.Shared)
	if err != nil {
		writeError(w, 500, "failed to update workspace scope")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "repository not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SetCodeGraphProjectAssignment(w http.ResponseWriter, r *http.Request) {
	ws := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, ws) {
		return
	}
	repo, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	project, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "projectId"), "project id")
	if !ok {
		return
	}
	// Lock the parent rows so concurrent deletion cannot leave an orphan grant.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to update project scope")
		return
	}
	defer tx.Rollback(r.Context())
	var id pgtype.UUID
	err = tx.QueryRow(r.Context(), `SELECT id FROM code_graph_repository WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, repo, parseUUID(ws)).Scan(&id)
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT id FROM project WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, project, parseUUID(ws)).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "repository or project not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to validate project scope")
		return
	}
	if r.Method == http.MethodDelete {
		_, err = tx.Exec(r.Context(), `DELETE FROM project_code_graph_repository WHERE repository_id=$1 AND project_id=$2`, repo, project)
	} else {
		_, err = tx.Exec(r.Context(), `INSERT INTO project_code_graph_repository(repository_id,project_id,created_by) VALUES($1,$2,$3) ON CONFLICT(repository_id,project_id) DO NOTHING`, repo, project, parseUUID(requestUserID(r)))
	}
	if err != nil {
		writeError(w, 500, "failed to update project scope")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to save project scope")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AvailableCodeGraphs describes current authorization, not a historical read log.
func (h *Handler) AvailableCodeGraphs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ws := workspaceIDFromURL(r, "id")
	if _, ok := h.workspaceMember(w, r, ws); !ok {
		return
	}
	task, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("task_id"), "task id")
	if !ok {
		return
	}
	var agent pgtype.UUID
	err := h.DB.QueryRow(r.Context(), `SELECT t.agent_id FROM agent_task_queue t JOIN agent a ON a.id=t.agent_id WHERE t.id=$1 AND a.workspace_id=$2 AND a.archived_at IS NULL`, task, parseUUID(ws)).Scan(&agent)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "task not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to load task")
		return
	}
	access := service.CodeGraphAccessService{DB: h.DB, WorkspaceID: parseUUID(ws), AgentID: agent, TaskID: task}
	repos, err := access.Repositories(r.Context(), "")
	if err != nil {
		writeError(w, 500, "failed to resolve graph access")
		return
	}
	writeJSON(w, 200, repos)
}

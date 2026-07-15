package apps

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) ListFolders(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,parent_id,name FROM cerebro_app_folder WHERE workspace_id=$1 ORDER BY lower(name)`, workspaceID)
	if err != nil {
		writeError(w, 500, "failed to list app folders")
		return
	}
	defer rows.Close()
	folders := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var parent *uuid.UUID
		var name string
		if rows.Scan(&id, &parent, &name) != nil {
			writeError(w, 500, "failed to read app folders")
			return
		}
		folders = append(folders, map[string]any{"id": id, "parent_id": parent, "name": name})
	}
	writeJSON(w, 200, map[string]any{"folders": folders})
}

func (h *Handler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	var req folderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, 400, "folder name is required")
		return
	}
	parent, valid := parseOptionalFolderID(w, req.ParentID)
	if !valid {
		return
	}
	var id uuid.UUID
	err := h.pool.QueryRow(r.Context(), `INSERT INTO cerebro_app_folder(workspace_id,parent_id,name) SELECT $1,$2,$3 WHERE $2::uuid IS NULL OR EXISTS(SELECT 1 FROM cerebro_app_folder WHERE id=$2 AND workspace_id=$1) RETURNING id`, workspaceID, parent, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 400, "parent folder not found")
		return
	}
	if err != nil {
		writeError(w, 409, "folder already exists")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "parent_id": parent, "name": name})
}

func (h *Handler) UpdateFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "folderId"))
	if err != nil {
		writeError(w, 400, "invalid folder id")
		return
	}
	var req folderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, 400, "folder name is required")
		return
	}
	parent, valid := parseOptionalFolderID(w, req.ParentID)
	if !valid {
		return
	}
	result, err := h.pool.Exec(r.Context(), `WITH RECURSIVE descendants AS (SELECT id FROM cerebro_app_folder WHERE id=$1 UNION ALL SELECT f.id FROM cerebro_app_folder f JOIN descendants d ON f.parent_id=d.id) UPDATE cerebro_app_folder SET name=$4,parent_id=$3,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND ($3::uuid IS NULL OR ($3<>$1 AND NOT EXISTS(SELECT 1 FROM descendants WHERE id=$3) AND EXISTS(SELECT 1 FROM cerebro_app_folder WHERE id=$3 AND workspace_id=$2)))`, id, workspaceID, parent, name)
	if err != nil {
		writeError(w, 409, "folder could not be updated")
		return
	}
	if result.RowsAffected() != 1 {
		writeError(w, 400, "folder move would create a cycle")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "parent_id": parent, "name": name})
}

func (h *Handler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "folderId"))
	if err != nil {
		writeError(w, 400, "invalid folder id")
		return
	}
	result, err := h.pool.Exec(r.Context(), `DELETE FROM cerebro_app_folder WHERE id=$1 AND workspace_id=$2`, id, workspaceID)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, 404, "app folder not found")
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) MoveAppToFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	folderID, err := uuid.Parse(chi.URLParam(r, "folderId"))
	if err != nil {
		writeError(w, 400, "invalid folder id")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "appId"))
	if err != nil {
		writeError(w, 400, "invalid app id")
		return
	}
	result, err := h.pool.Exec(r.Context(), `UPDATE cerebro_app SET folder_id=$1,folder='',updated_at=now() WHERE id=$2 AND workspace_id=$3 AND EXISTS(SELECT 1 FROM cerebro_app_folder WHERE id=$1 AND workspace_id=$3)`, folderID, appID, workspaceID)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, 404, "app or folder not found")
		return
	}
	w.WriteHeader(204)
}

func parseOptionalFolderID(w http.ResponseWriter, raw *string) (*uuid.UUID, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		writeError(w, 400, "invalid parent folder id")
		return nil, false
	}
	return &id, true
}

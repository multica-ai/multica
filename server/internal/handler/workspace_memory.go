package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type MemoryIndexEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

type MemoryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	ProjectID   *string `json:"project_id"`
}

func memoryToResponse(m db.WorkspaceMemory) MemoryResponse {
	return MemoryResponse{
		ID:          uuidToString(m.ID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		Name:        m.Name,
		Description: m.Description,
		Content:     m.Content,
		CreatedBy:   uuidToPtr(m.CreatedBy),
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
		ProjectID:   uuidToPtr(m.ProjectID),
	}
}

func (h *Handler) ListWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	params := db.ListWorkspaceMemoryIndexParams{
		WorkspaceID: parseUUID(workspaceID),
	}
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		params.ProjectID = parseUUID(projectID)
	}

	rows, err := h.Queries.ListWorkspaceMemoryIndex(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list memory")
		return
	}

	resp := make([]MemoryIndexEntry, len(rows))
	for i, row := range rows {
		resp[i] = MemoryIndexEntry{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
			UpdatedAt:   timestampToString(row.UpdatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	id := chi.URLParam(r, "memoryID")
	entry, err := h.Queries.GetWorkspaceMemory(r.Context(), db.GetWorkspaceMemoryParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory entry not found")
		return
	}
	writeJSON(w, http.StatusOK, memoryToResponse(entry))
}

type CreateMemoryRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	ProjectID   *string `json:"project_id"`
}

func (h *Handler) CreateWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req CreateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var entry db.WorkspaceMemory
	var err error
	if req.ProjectID != nil && *req.ProjectID != "" {
		entry, err = h.Queries.CreateWorkspaceMemoryForProject(r.Context(), db.CreateWorkspaceMemoryForProjectParams{
			WorkspaceID: parseUUID(workspaceID),
			Name:        req.Name,
			Description: req.Description,
			Content:     req.Content,
			CreatedBy:   parseUUID(userID),
			ProjectID:   parseUUID(*req.ProjectID),
		})
	} else {
		entry, err = h.Queries.CreateWorkspaceMemory(r.Context(), db.CreateWorkspaceMemoryParams{
			WorkspaceID: parseUUID(workspaceID),
			Name:        req.Name,
			Description: req.Description,
			Content:     req.Content,
			CreatedBy:   parseUUID(userID),
		})
	}
	if err != nil {
		slog.Warn("create workspace memory failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create memory entry")
		return
	}
	writeJSON(w, http.StatusCreated, memoryToResponse(entry))
}

type UpdateMemoryRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
}

func (h *Handler) UpdateWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	id := chi.URLParam(r, "memoryID")

	var req UpdateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkspaceMemoryParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
	}

	entry, err := h.Queries.UpdateWorkspaceMemory(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update memory entry")
		return
	}
	writeJSON(w, http.StatusOK, memoryToResponse(entry))
}

func (h *Handler) DeleteWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can delete memory entries")
		return
	}

	id := chi.URLParam(r, "memoryID")
	if err := h.Queries.DeleteWorkspaceMemory(r.Context(), db.DeleteWorkspaceMemoryParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete memory entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ProjectDocumentResponse struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	ParentID  *string `json:"parent_id"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	SortOrder int32   `json:"sort_order"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func projectDocumentToResponse(d db.ProjectDocument) ProjectDocumentResponse {
	return ProjectDocumentResponse{
		ID:        uuidToString(d.ID),
		ProjectID: uuidToString(d.ProjectID),
		ParentID:  uuidToPtr(d.ParentID),
		Title:     d.Title,
		Content:   d.Content,
		SortOrder: d.SortOrder,
		CreatedBy: uuidToString(d.CreatedBy),
		CreatedAt: timestampToString(d.CreatedAt),
		UpdatedAt: timestampToString(d.UpdatedAt),
	}
}

func (h *Handler) ListProjectDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}

	documents, err := h.Queries.ListDocumentsByProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project documents")
		return
	}

	resp := make([]ProjectDocumentResponse, len(documents))
	for i, d := range documents {
		resp[i] = projectDocumentToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateProjectDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	
	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		ParentID  *string `json:"parent_id"`
		Title     string  `json:"title"`
		Content   string  `json:"content"`
		SortOrder int32   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var parentUUID pgtype.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parentUUID = parsed
	}

	d, err := h.Queries.CreateDocument(r.Context(), db.CreateDocumentParams{
		ProjectID: projectID,
		ParentID:  parentUUID,
		Title:     req.Title,
		Content:   req.Content,
		SortOrder: req.SortOrder,
		CreatedBy: requester.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project document")
		return
	}

	resp := projectDocumentToResponse(d)
	writeJSON(w, http.StatusCreated, resp)
	
	h.publish(protocol.EventProjectDocumentCreated, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"document": resp,
	})
}

func (h *Handler) GetProjectDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	documentIdStr := chi.URLParam(r, "documentId")
	documentUUID, ok := parseUUIDOrBadRequest(w, documentIdStr, "document id")
	if !ok {
		return
	}

	d, err := h.Queries.GetDocument(r.Context(), documentUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	writeJSON(w, http.StatusOK, projectDocumentToResponse(d))
}

func (h *Handler) UpdateProjectDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	documentIdStr := chi.URLParam(r, "documentId")
	documentUUID, ok := parseUUIDOrBadRequest(w, documentIdStr, "document id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		ParentID  *string `json:"parent_id"`
		Title     *string `json:"title"`
		Content   *string `json:"content"`
		SortOrder *int32  `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateDocumentParams{
		ID: documentUUID,
	}
	
	if req.ParentID != nil {
		if *req.ParentID == "" {
			params.ParentID = pgtype.UUID{Valid: false}
		} else {
			parsed, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
			if !ok {
				return
			}
			params.ParentID = parsed
		}
	}
	
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
	}
	if req.SortOrder != nil {
		params.SortOrder = pgtype.Int4{Int32: *req.SortOrder, Valid: true}
	}

	d, err := h.Queries.UpdateDocument(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project document")
		return
	}

	resp := projectDocumentToResponse(d)
	writeJSON(w, http.StatusOK, resp)
	
	h.publish(protocol.EventProjectDocumentUpdated, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"document": resp,
	})
}

func (h *Handler) DeleteProjectDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	documentIdStr := chi.URLParam(r, "documentId")
	documentUUID, ok := parseUUIDOrBadRequest(w, documentIdStr, "document id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	err := h.Queries.DeleteDocument(r.Context(), documentUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project document")
		return
	}

	w.WriteHeader(http.StatusNoContent)
	
	h.publish(protocol.EventProjectDocumentDeleted, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"document_id": documentIdStr,
	})
}

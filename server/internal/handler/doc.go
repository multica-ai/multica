package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DocResponse is the JSON shape returned for a workspace doc.
type DocResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	CreatorID   string  `json:"creator_id"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	ParentID    *string `json:"parent_id"`
	Position    float64 `json:"position"`
	IsArchived  bool    `json:"is_archived"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func docToResponse(d db.Doc) DocResponse {
	return DocResponse{
		ID:          uuidToString(d.ID),
		WorkspaceID: uuidToString(d.WorkspaceID),
		CreatorID:   uuidToString(d.CreatorID),
		Title:       d.Title,
		Content:     d.Content,
		ParentID:    uuidToPtr(d.ParentID),
		Position:    d.Position,
		IsArchived:  d.IsArchived,
		CreatedAt:   timestampToString(d.CreatedAt),
		UpdatedAt:   timestampToString(d.UpdatedAt),
	}
}

// ListDocs returns all non-archived docs in the workspace.
func (h *Handler) ListDocs(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	docs, err := h.Queries.ListDocs(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list docs")
		return
	}

	resp := make([]DocResponse, len(docs))
	for i, d := range docs {
		resp[i] = docToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetDoc returns a single doc.
func (h *Handler) GetDoc(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	docID := chi.URLParam(r, "docId")
	docUUID, ok := parseUUIDOrBadRequest(w, docID, "doc_id")
	if !ok {
		return
	}

	doc, err := h.Queries.GetDoc(r.Context(), db.GetDocParams{
		ID:          docUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	writeJSON(w, http.StatusOK, docToResponse(doc))
}

// CreateDoc creates a new doc.
func (h *Handler) CreateDoc(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Title    string  `json:"title"`
		Content  string  `json:"content"`
		ParentID *string `json:"parent_id"`
		Position float64 `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var parentID pgtype.UUID
	if body.ParentID != nil {
		pid, err := util.ParseUUID(*body.ParentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid parent_id")
			return
		}
		parentID = pid
	}

	doc, err := h.Queries.CreateDoc(r.Context(), db.CreateDocParams{
		WorkspaceID: wsUUID,
		CreatorID:   member.UserID,
		Title:       body.Title,
		Content:     body.Content,
		ParentID:    parentID,
		Position:    body.Position,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	writeJSON(w, http.StatusCreated, docToResponse(doc))
}

// UpdateDoc updates title, content, parent, or position of a doc.
func (h *Handler) UpdateDoc(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	docID := chi.URLParam(r, "docId")
	docUUID, ok := parseUUIDOrBadRequest(w, docID, "doc_id")
	if !ok {
		return
	}

	var body struct {
		Title    *string  `json:"title"`
		Content  *string  `json:"content"`
		ParentID *string  `json:"parent_id"`
		Position *float64 `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var parentID pgtype.UUID
	if body.ParentID != nil {
		pid, err := util.ParseUUID(*body.ParentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid parent_id")
			return
		}
		parentID = pid
	}

	var title pgtype.Text
	if body.Title != nil {
		title = pgtype.Text{String: *body.Title, Valid: true}
	}
	var content pgtype.Text
	if body.Content != nil {
		content = pgtype.Text{String: *body.Content, Valid: true}
	}
	var position pgtype.Float8
	if body.Position != nil {
		position = pgtype.Float8{Float64: *body.Position, Valid: true}
	}

	doc, err := h.Queries.UpdateDoc(r.Context(), db.UpdateDocParams{
		ID:          docUUID,
		WorkspaceID: wsUUID,
		Title:       title,
		Content:     content,
		ParentID:    parentID,
		Position:    position,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	writeJSON(w, http.StatusOK, docToResponse(doc))
}

// ArchiveDoc soft-deletes a doc.
func (h *Handler) ArchiveDoc(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	docID := chi.URLParam(r, "docId")
	docUUID, ok := parseUUIDOrBadRequest(w, docID, "doc_id")
	if !ok {
		return
	}

	doc, err := h.Queries.ArchiveDoc(r.Context(), db.ArchiveDocParams{
		ID:          docUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	writeJSON(w, http.StatusOK, docToResponse(doc))
}

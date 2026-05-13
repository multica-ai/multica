package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type WikiPageResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Title         string  `json:"title"`
	Content       string  `json:"content"`
	Slug          *string `json:"slug"`
	CreatedByType string  `json:"created_by_type"`
	CreatedByID   string  `json:"created_by_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func wikiPageToResponse(p db.WikiPage) WikiPageResponse {
	return WikiPageResponse{
		ID:            uuidToString(p.ID),
		WorkspaceID:   uuidToString(p.WorkspaceID),
		Title:         p.Title,
		Content:       p.Content,
		Slug:          textToPtr(p.Slug),
		CreatedByType: p.CreatedByType,
		CreatedByID:   uuidToString(p.CreatedByID),
		CreatedAt:     timestampToString(p.CreatedAt),
		UpdatedAt:     timestampToString(p.UpdatedAt),
	}
}

func wikiPagesToResponse(list []db.WikiPage) []WikiPageResponse {
	out := make([]WikiPageResponse, len(list))
	for i, p := range list {
		out[i] = wikiPageToResponse(p)
	}
	return out
}

type CreateWikiPageRequest struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	Slug    *string `json:"slug"`
}

type UpdateWikiPageRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Slug    *string `json:"slug"`
}

const (
	maxWikiTitleLen   = 500
	maxWikiContentLen = 100 * 1024 // 100 KB
)

// slugRE matches valid wiki slugs: lowercase alphanumeric characters and hyphens.
var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// validateWikiSlug trims and validates a slug. Returns the trimmed slug or
// an error suitable for a 400 response. An empty slug is valid (means "no slug").
func validateWikiSlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if !slugRE.MatchString(s) {
		return "", errors.New("slug must contain only lowercase letters, numbers, and hyphens")
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Handlers — wiki CRUD
// ---------------------------------------------------------------------------

func (h *Handler) ListWikiPages(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	pages, err := h.Queries.ListWikiPages(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListWikiPages failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list wiki pages")
		return
	}
	resp := wikiPagesToResponse(pages)
	writeJSON(w, http.StatusOK, map[string]any{"wiki_pages": resp, "total": len(resp)})
}

func (h *Handler) GetWikiPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	idUUID, ok := parseUUIDOrBadRequest(w, id, "wiki page id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	page, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "wiki page not found")
			return
		}
		slog.Warn("GetWikiPage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get wiki page")
		return
	}
	writeJSON(w, http.StatusOK, wikiPageToResponse(page))
}

func (h *Handler) CreateWikiPage(w http.ResponseWriter, r *http.Request) {
	var req CreateWikiPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(title) > maxWikiTitleLen {
		writeError(w, http.StatusBadRequest, "title must be 500 characters or fewer")
		return
	}
	if len(req.Content) > maxWikiContentLen {
		writeError(w, http.StatusBadRequest, "content exceeds maximum allowed size")
		return
	}

	var slugText pgtype.Text
	if req.Slug != nil {
		s, err := validateWikiSlug(*req.Slug)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if s != "" {
			slugText = pgtype.Text{String: s, Valid: true}
		}
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	page, err := h.Queries.CreateWikiPage(r.Context(), db.CreateWikiPageParams{
		WorkspaceID:   wsUUID,
		Title:         title,
		Content:       req.Content,
		Slug:          slugText,
		CreatedByType: actorType,
		CreatedByID:   actorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a wiki page with that slug already exists")
			return
		}
		slog.Warn("CreateWikiPage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create wiki page")
		return
	}

	resp := wikiPageToResponse(page)
	h.publish(protocol.EventWikiPageCreated, workspaceID, actorType, actorID, map[string]any{"wiki_page": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWikiPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	var req UpdateWikiPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	idUUID, ok := parseUUIDOrBadRequest(w, id, "wiki page id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	params := db.UpdateWikiPageParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		if len(title) > maxWikiTitleLen {
			writeError(w, http.StatusBadRequest, "title must be 500 characters or fewer")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}

	if req.Content != nil {
		if len(*req.Content) > maxWikiContentLen {
			writeError(w, http.StatusBadRequest, "content exceeds maximum allowed size")
			return
		}
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
	}

	// Slug is always passed to UpdateWikiPage (sqlc.narg): a nil pointer clears
	// the slug; a non-nil pointer sets (or updates) it.
	if req.Slug != nil {
		s, err := validateWikiSlug(*req.Slug)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if s != "" {
			params.Slug = pgtype.Text{String: s, Valid: true}
		}
		// s == "" means caller sent slug: "" — treat as clear (params.Slug stays zero/invalid)
	}

	page, err := h.Queries.UpdateWikiPage(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "wiki page not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a wiki page with that slug already exists")
			return
		}
		slog.Warn("UpdateWikiPage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update wiki page")
		return
	}

	resp := wikiPageToResponse(page)
	h.publish(protocol.EventWikiPageUpdated, workspaceID, actorType, actorID, map[string]any{"wiki_page": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	idUUID, ok := parseUUIDOrBadRequest(w, id, "wiki page id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.DeleteWikiPage(r.Context(), db.DeleteWikiPageParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "wiki page not found")
			return
		}
		slog.Warn("DeleteWikiPage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete wiki page")
		return
	}

	h.publish(protocol.EventWikiPageDeleted, workspaceID, actorType, actorID, map[string]any{"wiki_page_id": uuidToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

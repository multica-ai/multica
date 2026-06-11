package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var wikiSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

type WikiPageSummaryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ParentID    *string `json:"parent_id"`
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	Type        string  `json:"type"`
	Position    float64 `json:"position"`
	CreatedBy   *string `json:"created_by"`
	UpdatedBy   *string `json:"updated_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type WikiPageResponse struct {
	WikiPageSummaryResponse
	Content string `json:"content"`
}

func wikiPageSummaryRowToResponse(p db.ListWikiPagesRow) WikiPageSummaryResponse {
	return WikiPageSummaryResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		ParentID:    uuidToPtr(p.ParentID),
		Title:       p.Title,
		Slug:        p.Slug,
		Type:        p.Type,
		Position:    p.Position,
		CreatedBy:   uuidToPtr(p.CreatedBy),
		UpdatedBy:   uuidToPtr(p.UpdatedBy),
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

func wikiPageToSummaryResponse(p db.WikiPage) WikiPageSummaryResponse {
	return WikiPageSummaryResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		ParentID:    uuidToPtr(p.ParentID),
		Title:       p.Title,
		Slug:        p.Slug,
		Type:        p.Type,
		Position:    p.Position,
		CreatedBy:   uuidToPtr(p.CreatedBy),
		UpdatedBy:   uuidToPtr(p.UpdatedBy),
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

func wikiPageToResponse(p db.WikiPage) WikiPageResponse {
	return WikiPageResponse{
		WikiPageSummaryResponse: wikiPageToSummaryResponse(p),
		Content:                 p.Content,
	}
}

func wikiSlugFromTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = wikiSlugNonAlnum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "page"
	}
	return slug
}

func wikiSlugWithSuffix(base string, attempt int) string {
	if attempt <= 1 {
		return base
	}
	return base + "-" + strconv.Itoa(attempt)
}

func (h *Handler) requireWikiAdmin(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	if m, ok := ctxMember(r.Context()); ok {
		if roleAllowed(m.Role, "owner", "admin") {
			return m, true
		}
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, false
	}
	return h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
}

func (h *Handler) ListWikiPages(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	pages, err := h.Queries.ListWikiPages(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wiki pages")
		return
	}
	resp := make([]WikiPageSummaryResponse, len(pages))
	for i, page := range pages {
		resp[i] = wikiPageSummaryRowToResponse(page)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": resp, "total": len(resp)})
}

func (h *Handler) GetWikiPage(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "wiki page id")
	if !ok {
		return
	}
	page, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "wiki page not found")
		return
	}
	writeJSON(w, http.StatusOK, wikiPageToResponse(page))
}

type CreateWikiPageRequest struct {
	Title    string   `json:"title"`
	ParentID *string  `json:"parent_id"`
	Type     *string  `json:"type"`
	Content  *string  `json:"content"`
	Position *float64 `json:"position"`
}

func (h *Handler) CreateWikiPage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWikiAdmin(w, r, workspaceID)
	if !ok {
		return
	}

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

	// Validate type field (default to "page")
	pageType := "page"
	if req.Type != nil {
		t := strings.TrimSpace(*req.Type)
		if t != "page" && t != "folder" {
			writeError(w, http.StatusBadRequest, "type must be 'page' or 'folder'")
			return
		}
		pageType = t
	}

	var parentID pgtype.UUID
	if req.ParentID != nil && strings.TrimSpace(*req.ParentID) != "" {
		// Folders cannot have a parent (no nesting)
		if pageType == "folder" {
			writeError(w, http.StatusBadRequest, "folders cannot be nested inside other folders")
			return
		}
		parsedParentID, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parentPage, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{ID: parsedParentID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "parent wiki page not found")
			return
		}
		// Only folders can be parents (one-level constraint)
		if parentPage.Type != "folder" {
			writeError(w, http.StatusBadRequest, "parent must be a folder")
			return
		}
		parentID = parsedParentID
	}

	position := 0.0
	if req.Position != nil {
		position = *req.Position
	} else {
		maxPosition, err := h.Queries.GetMaxWikiPagePosition(r.Context(), db.GetMaxWikiPagePositionParams{
			WorkspaceID: wsUUID,
			ParentID:    parentID,
		})
		if err == nil {
			position = maxPosition + 1
		}
	}

	var content pgtype.Text
	if req.Content != nil {
		content = pgtype.Text{String: *req.Content, Valid: true}
	}
	pageTypeText := pgtype.Text{String: pageType, Valid: true}
	baseSlug := wikiSlugFromTitle(title)
	var page db.WikiPage
	var err error
	for attempt := 1; attempt <= 20; attempt++ {
		page, err = h.Queries.CreateWikiPage(r.Context(), db.CreateWikiPageParams{
			WorkspaceID: wsUUID,
			ParentID:    parentID,
			Title:       title,
			Slug:        wikiSlugWithSuffix(baseSlug, attempt),
			Content:     content,
			Type:        pageTypeText,
			Position:    position,
			CreatedBy:   member.UserID,
			UpdatedBy:   member.UserID,
		})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to create wiki page")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusConflict, "wiki page slug already exists")
		return
	}

	resp := wikiPageToResponse(page)
	h.recordWikiActivity(r, wsUUID, page.ID, member.UserID, "created", map[string]any{"title": title})
	h.publish(protocol.EventWikiPageCreated, workspaceID, "member", requestUserID(r), map[string]any{"page": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateWikiPageRequest struct {
	Title    *string  `json:"title"`
	Content  *string  `json:"content"`
	Position *float64 `json:"position"`
	ParentID *string  `json:"parent_id"`
}

func (h *Handler) UpdateWikiPage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWikiAdmin(w, r, workspaceID)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "wiki page id")
	if !ok {
		return
	}

	var req UpdateWikiPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWikiPageParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
		UpdatedBy:   member.UserID,
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	// Handle parent_id change (move between folders)
	if req.ParentID != nil {
		pid := strings.TrimSpace(*req.ParentID)
		if pid == "" {
			// Move to top level (clear parent)
			params.ParentID = pgtype.UUID{Valid: false}
		} else {
			// Validate that the target parent exists and is a folder
			parsedParentID, ok := parseUUIDOrBadRequest(w, pid, "parent_id")
			if !ok {
				return
			}
			parentPage, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{ID: parsedParentID, WorkspaceID: wsUUID})
			if err != nil {
				writeError(w, http.StatusBadRequest, "parent wiki page not found")
				return
			}
			if parentPage.Type != "folder" {
				writeError(w, http.StatusBadRequest, "parent must be a folder")
				return
			}
			// Prevent moving a folder into another folder
			currentPage, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{ID: idUUID, WorkspaceID: wsUUID})
			if err != nil {
				writeError(w, http.StatusNotFound, "wiki page not found")
				return
			}
			if currentPage.Type == "folder" {
				writeError(w, http.StatusBadRequest, "folders cannot be nested inside other folders")
				return
			}
			params.ParentID = parsedParentID
		}
	}

	baseSlug := ""
	if params.Title.Valid {
		baseSlug = wikiSlugFromTitle(params.Title.String)
	}
	var page db.WikiPage
	var err error
	for attempt := 1; attempt <= 20; attempt++ {
		if baseSlug != "" {
			params.Slug = pgtype.Text{String: wikiSlugWithSuffix(baseSlug, attempt), Valid: true}
		}
		page, err = h.Queries.UpdateWikiPage(r.Context(), params)
		if err == nil {
			break
		}
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "wiki page not found")
			return
		}
		if baseSlug == "" || !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to update wiki page")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusConflict, "wiki page slug already exists")
		return
	}

	resp := wikiPageToResponse(page)
	activityAction := "updated"
	details := map[string]any{}
	if req.Title != nil {
		activityAction = "title_updated"
		details["title"] = *req.Title
	}
	if req.Content != nil {
		activityAction = "content_updated"
	}
	if req.Title != nil && req.Content != nil {
		activityAction = "updated"
		details["title"] = *req.Title
	}
	h.recordWikiActivity(r, wsUUID, page.ID, member.UserID, activityAction, details)
	h.publish(protocol.EventWikiPageUpdated, workspaceID, "member", requestUserID(r), map[string]any{"page": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWikiAdmin(w, r, workspaceID)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "wiki page id")
	if !ok {
		return
	}
	page, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "wiki page not found")
		return
	}
	childCount, err := h.Queries.CountWikiPageChildren(r.Context(), db.CountWikiPageChildrenParams{
		ParentID:    idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete wiki page")
		return
	}
	// Record activity before deletion (cascade will remove the activity record too,
	// but parent page activity could still be useful for logging)
	h.recordWikiActivity(r, wsUUID, page.ID, member.UserID, "deleted", map[string]any{"title": page.Title})
	if err := h.Queries.DeleteWikiPage(r.Context(), db.DeleteWikiPageParams{ID: idUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete wiki page")
		return
	}

	h.publish(protocol.EventWikiPageDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"page_id":     uuidToString(page.ID),
		"parent_id":   uuidToPtr(page.ParentID),
		"child_count": childCount,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "page_id": uuidToString(page.ID), "child_count": childCount})
}

type ReorderWikiPagesRequest struct {
	Pages []struct {
		ID       string   `json:"id"`
		Position float64  `json:"position"`
		ParentID *string  `json:"parent_id"`
	} `json:"pages"`
}

func (h *Handler) ReorderWikiPages(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWikiAdmin(w, r, workspaceID)
	if !ok {
		return
	}
	var req ReorderWikiPagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Pages) == 0 {
		writeError(w, http.StatusBadRequest, "pages are required")
		return
	}

	updated := make([]WikiPageSummaryResponse, 0, len(req.Pages))
	for _, item := range req.Pages {
		idUUID, ok := parseUUIDOrBadRequest(w, item.ID, "wiki page id")
		if !ok {
			return
		}
		var parentID pgtype.UUID
		if item.ParentID != nil && strings.TrimSpace(*item.ParentID) != "" {
			pid := strings.TrimSpace(*item.ParentID)
			parsedParentID, parseOk := parseUUIDOrBadRequest(w, pid, "parent_id")
			if !parseOk {
				return
			}
			parentPage, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{ID: parsedParentID, WorkspaceID: wsUUID})
			if err != nil {
				writeError(w, http.StatusBadRequest, "parent wiki page not found")
				return
			}
			if parentPage.Type != "folder" {
				writeError(w, http.StatusBadRequest, "parent must be a folder")
				return
			}
			parentID = parsedParentID
		}
		page, err := h.Queries.ReorderWikiPage(r.Context(), db.ReorderWikiPageParams{
			ID:          idUUID,
			WorkspaceID: wsUUID,
			Position:    item.Position,
			ParentID:    parentID,
			UpdatedBy:   member.UserID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "wiki page not found")
			return
		}
		updated = append(updated, wikiPageToSummaryResponse(page))
	}

	h.publish(protocol.EventWikiPageReordered, workspaceID, "member", requestUserID(r), map[string]any{"pages": updated})
	writeJSON(w, http.StatusOK, map[string]any{"pages": updated})
}

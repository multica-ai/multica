package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const viewNameMaxLen = 100

// validViewPages are the product surfaces a saved view can belong to. A view's
// page determines its base issue set; `filters` narrows within that set.
var validViewPages = map[string]bool{"issues": true, "my_issues": true, "project": true}

// knownViewFilterKeys mirrors the orthogonal GET /api/issues filter params plus
// the any_of OR-group. Saving an unknown key is rejected so a typo can't create
// a view that silently matches everything.
var knownViewFilterKeys = map[string]bool{
	"statuses":            true,
	"priorities":          true,
	"assignee_types":      true,
	"assignee_filters":    true,
	"include_no_assignee": true,
	"creator_filters":     true,
	"project_ids":         true,
	"include_no_project":  true,
	"label_ids":           true,
	"any_of":              true,
}

// ViewResponse is the JSON response for a saved view.
type ViewResponse struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	CreatorID   *string        `json:"creator_id"`
	Name        string         `json:"name"`
	Page        string         `json:"page"`
	ProjectID   *string        `json:"project_id"`
	Filters     map[string]any `json:"filters"`
	Display     map[string]any `json:"display"`
	Position    float64        `json:"position"`
	Shared      bool           `json:"shared"`
	IsDefault   bool           `json:"is_default"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func viewToResponse(v db.SavedView) ViewResponse {
	return ViewResponse{
		ID:          uuidToString(v.ID),
		WorkspaceID: uuidToString(v.WorkspaceID),
		CreatorID:   uuidToPtr(v.CreatorID),
		Name:        v.Name,
		Page:        v.Page,
		ProjectID:   uuidToPtr(v.ProjectID),
		Filters:     jsonbToMap(v.Filters),
		Display:     jsonbToMap(v.Display),
		Position:    v.Position,
		Shared:      v.Shared,
		IsDefault:   v.IsDefault,
		CreatedAt:   timestampToString(v.CreatedAt),
		UpdatedAt:   timestampToString(v.UpdatedAt),
	}
}

func jsonbToMap(b []byte) map[string]any {
	m := map[string]any{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// validateViewFilters checks the saved filter object is well-formed: a JSON
// object whose keys are all known issue-filter params, with any_of an array.
// Returns the canonical bytes to store (defaulting to "{}" when absent).
func validateViewFilters(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte("{}"), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New("filters must be a JSON object")
	}
	for key, val := range obj {
		if !knownViewFilterKeys[key] {
			return nil, errors.New("unknown filter key: " + key)
		}
		if key == "any_of" {
			var branches []map[string]json.RawMessage
			if err := json.Unmarshal(val, &branches); err != nil {
				return nil, errors.New("any_of must be an array of filter objects")
			}
			// Branches are flat filter objects; validate their keys too so an
			// unknown key can't hide inside an OR branch, and reject nesting.
			for _, branch := range branches {
				for bkey := range branch {
					if bkey == "any_of" {
						return nil, errors.New("any_of branches cannot themselves contain any_of")
					}
					if !knownViewFilterKeys[bkey] {
						return nil, errors.New("unknown filter key in any_of branch: " + bkey)
					}
				}
			}
		}
	}
	return raw, nil
}

func validateViewDisplay(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte("{}"), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New("display must be a JSON object")
	}
	return raw, nil
}

type CreateViewRequest struct {
	Name      string          `json:"name"`
	Page      string          `json:"page"`
	ProjectID *string         `json:"project_id"`
	Filters   json.RawMessage `json:"filters"`
	Display   json.RawMessage `json:"display"`
	Position  *float64        `json:"position"`
	Shared    *bool           `json:"shared"`
}

type UpdateViewRequest struct {
	Name     *string         `json:"name"`
	Filters  json.RawMessage `json:"filters"`
	Display  json.RawMessage `json:"display"`
	Position *float64        `json:"position"`
	Shared   *bool           `json:"shared"`
}

type ReorderViewsRequest struct {
	IDs []string `json:"ids"`
}

// resolveViewProject validates the page/project_id pairing: the project page
// requires a project_id, every other page forbids one.
func resolveViewProject(w http.ResponseWriter, page string, projectID *string) (pgtype.UUID, bool) {
	hasProject := projectID != nil && strings.TrimSpace(*projectID) != ""
	if page == "project" {
		if !hasProject {
			writeError(w, http.StatusBadRequest, "project_id is required for the project page")
			return pgtype.UUID{}, false
		}
		return parseUUIDOrBadRequest(w, strings.TrimSpace(*projectID), "project_id")
	}
	if hasProject {
		writeError(w, http.StatusBadRequest, "project_id is only valid for the project page")
		return pgtype.UUID{}, false
	}
	return pgtype.UUID{}, true
}

func (h *Handler) ListViews(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	viewerUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	page := r.URL.Query().Get("page")
	if !validViewPages[page] {
		writeError(w, http.StatusBadRequest, "invalid page")
		return
	}
	var projectID pgtype.UUID
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		projectID, ok = parseUUIDOrBadRequest(w, pid, "project_id")
		if !ok {
			return
		}
	}

	views, err := h.Queries.ListViews(r.Context(), db.ListViewsParams{
		WorkspaceID: wsUUID,
		Page:        page,
		ProjectID:   projectID,
		ViewerID:    viewerUUID,
	})
	if err != nil {
		slog.Warn("ListViews failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list views")
		return
	}
	resp := make([]ViewResponse, len(views))
	for i, v := range views {
		resp[i] = viewToResponse(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": resp})
}

func (h *Handler) CreateView(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	var req CreateViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > viewNameMaxLen {
		writeError(w, http.StatusBadRequest, "name must be 1-100 characters")
		return
	}
	if !validViewPages[req.Page] {
		writeError(w, http.StatusBadRequest, "invalid page")
		return
	}
	projectID, ok := resolveViewProject(w, req.Page, req.ProjectID)
	if !ok {
		return
	}
	filters, err := validateViewFilters(req.Filters)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	display, err := validateViewDisplay(req.Display)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var position float64
	if req.Position != nil {
		position = *req.Position
	}

	view, err := h.Queries.CreateView(r.Context(), db.CreateViewParams{
		WorkspaceID: wsUUID,
		CreatorID:   userUUID,
		Name:        name,
		Page:        req.Page,
		ProjectID:   projectID,
		Filters:     filters,
		Display:     display,
		Position:    position,
		Shared:      req.Shared != nil && *req.Shared,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a view with that name already exists")
			return
		}
		slog.Warn("CreateView failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create view")
		return
	}
	resp := viewToResponse(view)
	h.publish(protocol.EventViewCreated, workspaceID, "member", userID, map[string]any{"view": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// loadViewForManage loads a view and authorizes the caller to mutate it: the
// creator or a workspace owner/admin. Writes the appropriate error and returns
// ok=false otherwise.
func (h *Handler) loadViewForManage(w http.ResponseWriter, r *http.Request) (db.SavedView, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.SavedView{}, "", false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "view id")
	if !ok {
		return db.SavedView{}, "", false
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "view not found")
	if !ok {
		return db.SavedView{}, "", false
	}
	view, err := h.Queries.GetView(r.Context(), db.GetViewParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "view not found")
			return db.SavedView{}, "", false
		}
		slog.Warn("GetView failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load view")
		return db.SavedView{}, "", false
	}
	userID := requestUserID(r)
	if uuidToString(view.CreatorID) != userID && !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only the view creator or a workspace admin can manage this view")
		return db.SavedView{}, "", false
	}
	return view, workspaceID, true
}

func (h *Handler) UpdateView(w http.ResponseWriter, r *http.Request) {
	view, workspaceID, ok := h.loadViewForManage(w, r)
	if !ok {
		return
	}
	var req UpdateViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateViewParams{ID: view.ID, WorkspaceID: view.WorkspaceID}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > viewNameMaxLen {
			writeError(w, http.StatusBadRequest, "name must be 1-100 characters")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Filters != nil {
		filters, err := validateViewFilters(req.Filters)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Filters = filters
	}
	if req.Display != nil {
		display, err := validateViewDisplay(req.Display)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Display = display
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	if req.Shared != nil {
		params.Shared = pgtype.Bool{Bool: *req.Shared, Valid: true}
	}

	updated, err := h.Queries.UpdateView(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a view with that name already exists")
			return
		}
		slog.Warn("UpdateView failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update view")
		return
	}
	resp := viewToResponse(updated)
	h.publish(protocol.EventViewUpdated, workspaceID, "member", requestUserID(r), map[string]any{"view": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteView(w http.ResponseWriter, r *http.Request) {
	view, workspaceID, ok := h.loadViewForManage(w, r)
	if !ok {
		return
	}
	if view.IsDefault {
		writeError(w, http.StatusBadRequest, "a default view cannot be deleted")
		return
	}
	if _, err := h.Queries.DeleteView(r.Context(), db.DeleteViewParams{ID: view.ID, WorkspaceID: view.WorkspaceID}); err != nil {
		slog.Warn("DeleteView failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	h.publish(protocol.EventViewDeleted, workspaceID, "member", requestUserID(r), map[string]any{"view_id": uuidToString(view.ID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReorderViews(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req ReorderViewsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id, ok := parseUUIDOrBadRequest(w, raw, "ids")
		if !ok {
			return
		}
		ids = append(ids, id)
	}

	if err := h.Queries.ReorderViews(r.Context(), db.ReorderViewsParams{WorkspaceID: wsUUID, Ids: ids}); err != nil {
		slog.Warn("ReorderViews failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder views")
		return
	}
	h.publish(protocol.EventViewReordered, workspaceID, "member", userID, map[string]any{"ids": req.IDs})
	w.WriteHeader(http.StatusNoContent)
}

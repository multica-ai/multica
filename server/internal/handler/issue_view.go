package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxIssueViewsPerWorkspace  = 100
	maxIssueViewDefinitionSize = 64 << 10
	maxIssueViewRequestSize    = maxIssueViewDefinitionSize + (8 << 10)
)

type IssueViewResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	CreatorID   string          `json:"creator_id"`
	Name        string          `json:"name"`
	Icon        *string         `json:"icon"`
	Color       *string         `json:"color"`
	ScopeType   string          `json:"scope_type"`
	ScopeID     *string         `json:"scope_id"`
	Visibility  string          `json:"visibility"`
	Definition  json.RawMessage `json:"definition"`
	Position    float64         `json:"position"`
	CanEdit     bool            `json:"can_edit"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type issueViewCreateRequest struct {
	Name       string          `json:"name"`
	Icon       *string         `json:"icon"`
	Color      *string         `json:"color"`
	ScopeType  string          `json:"scope_type"`
	ScopeID    *string         `json:"scope_id"`
	Visibility string          `json:"visibility"`
	Definition json.RawMessage `json:"definition"`
}

type issueViewUpdateRequest struct {
	Name       *string                 `json:"name"`
	Icon       issueViewOptionalString `json:"icon"`
	Color      issueViewOptionalString `json:"color"`
	Visibility *string                 `json:"visibility"`
	Definition json.RawMessage         `json:"definition"`
}

// issueViewOptionalString distinguishes an omitted patch field from an
// explicit null, allowing clients to clear icon/color without accidentally
// clearing them on every partial update.
type issueViewOptionalString struct {
	Set   bool
	Value *string
}

func (value *issueViewOptionalString) UnmarshalJSON(data []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type issueViewDuplicateRequest struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

type issueViewDefaultRequest struct {
	ScopeType string  `json:"scope_type"`
	ScopeID   *string `json:"scope_id"`
	ViewID    *string `json:"view_id"`
}

type issueViewScope struct {
	typeName          string
	dbScopeID         pgtype.UUID
	preferenceScopeID pgtype.UUID
}

func issueViewCanEdit(view db.IssueView, member db.Member) bool {
	return view.CreatorID == member.UserID || roleAllowed(member.Role, "owner", "admin")
}

func issueViewToResponse(view db.IssueView, member db.Member) IssueViewResponse {
	definition := json.RawMessage(view.Definition)
	if len(definition) == 0 {
		definition = json.RawMessage(`{"version":1}`)
	}
	return IssueViewResponse{
		ID:          uuidToString(view.ID),
		WorkspaceID: uuidToString(view.WorkspaceID),
		CreatorID:   uuidToString(view.CreatorID),
		Name:        view.Name,
		Icon:        textToPtr(view.Icon),
		Color:       textToPtr(view.Color),
		ScopeType:   view.ScopeType,
		ScopeID:     uuidToPtr(view.ScopeID),
		Visibility:  view.Visibility,
		Definition:  definition,
		Position:    view.Position,
		CanEdit:     issueViewCanEdit(view, member),
		CreatedAt:   timestampToString(view.CreatedAt),
		UpdatedAt:   timestampToString(view.UpdatedAt),
	}
}

func decodeIssueViewJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxIssueViewRequestSize)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "view payload is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func validateIssueViewName(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	return trimmed, trimmed != "" && utf8.RuneCountInString(trimmed) <= 80
}

func validateIssueViewOptionalText(value *string, max int) bool {
	return value == nil || utf8.RuneCountInString(*value) <= max
}

func validateIssueViewVisibility(visibility string) bool {
	return visibility == "private" || visibility == "workspace"
}

func validateIssueViewDefinition(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("definition is required")
	}
	if len(trimmed) > maxIssueViewDefinitionSize {
		return nil, errors.New("definition exceeds 64 KiB")
	}
	var value map[string]any
	if err := json.Unmarshal(trimmed, &value); err != nil || value == nil {
		return nil, errors.New("definition must be a JSON object")
	}
	version, ok := value["version"].(float64)
	if !ok || version < 1 || version != float64(int64(version)) {
		return nil, errors.New("definition.version must be a positive integer")
	}
	return json.Marshal(value)
}

func (h *Handler) resolveIssueViewScope(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	userID string,
	scopeType string,
	scopeID *string,
) (issueViewScope, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return issueViewScope{}, false
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return issueViewScope{}, false
	}
	switch scopeType {
	case "workspace":
		if scopeID != nil && strings.TrimSpace(*scopeID) != "" {
			writeError(w, http.StatusBadRequest, "scope_id is not allowed for workspace views")
			return issueViewScope{}, false
		}
		return issueViewScope{typeName: scopeType, preferenceScopeID: wsUUID}, true
	case "my":
		if scopeID != nil && strings.TrimSpace(*scopeID) != "" {
			writeError(w, http.StatusBadRequest, "scope_id is not allowed for My Issues views")
			return issueViewScope{}, false
		}
		return issueViewScope{typeName: scopeType, preferenceScopeID: userUUID}, true
	case "project":
		if scopeID == nil || strings.TrimSpace(*scopeID) == "" {
			writeError(w, http.StatusBadRequest, "scope_id is required for project views")
			return issueViewScope{}, false
		}
		projectUUID, ok := parseUUIDOrBadRequest(w, *scopeID, "scope_id")
		if !ok {
			return issueViewScope{}, false
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: projectUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return issueViewScope{}, false
		}
		return issueViewScope{typeName: scopeType, dbScopeID: projectUUID, preferenceScopeID: projectUUID}, true
	default:
		writeError(w, http.StatusBadRequest, "scope_type must be 'workspace', 'project', or 'my'")
		return issueViewScope{}, false
	}
}

func (h *Handler) ListIssueViews(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	scopeType := strings.TrimSpace(r.URL.Query().Get("scope_type"))
	var scopeID *string
	if raw := strings.TrimSpace(r.URL.Query().Get("scope_id")); raw != "" {
		scopeID = &raw
	}
	scope, ok := h.resolveIssueViewScope(w, r, workspaceID, userID, scopeType, scopeID)
	if !ok {
		return
	}
	views, err := h.Queries.ListIssueViews(r.Context(), db.ListIssueViewsParams{
		WorkspaceID: parseUUID(workspaceID), ScopeType: scope.typeName,
		ScopeID: scope.dbScopeID, UserID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list views")
		return
	}
	resp := make([]IssueViewResponse, 0, len(views))
	visibleIDs := make(map[string]struct{}, len(views))
	for _, view := range views {
		resp = append(resp, issueViewToResponse(view, member))
		visibleIDs[uuidToString(view.ID)] = struct{}{}
	}
	var defaultViewID *string
	if id, err := h.Queries.GetDefaultIssueViewID(r.Context(), db.GetDefaultIssueViewIDParams{
		WorkspaceID: parseUUID(workspaceID), UserID: parseUUID(userID),
		ScopeType: scope.typeName, ScopeID: scope.preferenceScopeID,
	}); err == nil {
		value := uuidToString(id)
		if _, visible := visibleIDs[value]; visible {
			defaultViewID = &value
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to list views")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"views": resp, "default_view_id": defaultViewID,
	})
}

func (h *Handler) loadIssueViewForUser(w http.ResponseWriter, r *http.Request) (db.IssueView, db.Member, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.IssueView{}, db.Member{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return db.IssueView{}, db.Member{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "view id")
	if !ok {
		return db.IssueView{}, db.Member{}, false
	}
	view, err := h.Queries.GetIssueViewForUser(r.Context(), db.GetIssueViewForUserParams{
		ID: id, WorkspaceID: parseUUID(workspaceID), UserID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "view not found")
		return db.IssueView{}, db.Member{}, false
	}
	return view, member, true
}

func (h *Handler) GetIssueView(w http.ResponseWriter, r *http.Request) {
	view, member, ok := h.loadIssueViewForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, issueViewToResponse(view, member))
}

func (h *Handler) createIssueViewRow(
	ctx context.Context,
	workspaceID pgtype.UUID,
	creatorID pgtype.UUID,
	name string,
	icon *string,
	color *string,
	scope issueViewScope,
	visibility string,
	definition []byte,
) (db.IssueView, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.IssueView{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForIssueViewCreate(ctx, workspaceID); err != nil {
		return db.IssueView{}, err
	}
	// Project existence was checked before the transaction for a useful 404,
	// but must be checked again after taking the workspace lock. DeleteProject
	// takes the same lock, closing the no-FK race where a project could be
	// deleted between validation and insert and leave an orphaned view.
	if scope.typeName == "project" {
		if _, err := qtx.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID: scope.dbScopeID, WorkspaceID: workspaceID,
		}); err != nil {
			return db.IssueView{}, err
		}
	}
	count, err := qtx.CountIssueViewsByWorkspace(ctx, workspaceID)
	if err != nil {
		return db.IssueView{}, err
	}
	if count >= maxIssueViewsPerWorkspace {
		return db.IssueView{}, errors.New("view limit reached")
	}
	maxPosition, err := qtx.GetMaxIssueViewPosition(ctx, db.GetMaxIssueViewPositionParams{
		WorkspaceID: workspaceID, ScopeType: scope.typeName, ScopeID: scope.dbScopeID,
	})
	if err != nil {
		return db.IssueView{}, err
	}
	view, err := qtx.CreateIssueView(ctx, db.CreateIssueViewParams{
		WorkspaceID: workspaceID, CreatorID: creatorID, Name: name,
		Icon: textFromPtr(icon), Color: textFromPtr(color), ScopeType: scope.typeName,
		ScopeID: scope.dbScopeID, Visibility: visibility, Definition: definition,
		Position: maxPosition + 1,
	})
	if err != nil {
		return db.IssueView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.IssueView{}, err
	}
	return view, nil
}

func textFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func (h *Handler) CreateIssueView(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	var req issueViewCreateRequest
	if !decodeIssueViewJSON(w, r, &req) {
		return
	}
	name, valid := validateIssueViewName(req.Name)
	if !valid {
		writeError(w, http.StatusBadRequest, "name must be between 1 and 80 characters")
		return
	}
	if !validateIssueViewOptionalText(req.Icon, 64) || !validateIssueViewOptionalText(req.Color, 32) {
		writeError(w, http.StatusBadRequest, "icon or color is too long")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if !validateIssueViewVisibility(req.Visibility) {
		writeError(w, http.StatusBadRequest, "visibility must be 'private' or 'workspace'")
		return
	}
	scope, ok := h.resolveIssueViewScope(w, r, workspaceID, userID, req.ScopeType, req.ScopeID)
	if !ok {
		return
	}
	if scope.typeName == "my" && req.Visibility != "private" {
		writeError(w, http.StatusBadRequest, "My Issues views must be private")
		return
	}
	definition, err := validateIssueViewDefinition(req.Definition)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	view, err := h.createIssueViewRow(
		r.Context(), parseUUID(workspaceID), parseUUID(userID), name,
		req.Icon, req.Color, scope, req.Visibility, definition,
	)
	if err != nil {
		if err.Error() == "view limit reached" {
			writeError(w, http.StatusConflict, "a workspace cannot have more than 100 views")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace or project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create view")
		return
	}
	resp := issueViewToResponse(view, member)
	h.publish(protocol.EventIssueViewCreated, workspaceID, "member", userID, map[string]any{
		"view_id": uuidToString(view.ID), "visibility": view.Visibility,
		"recipient_id": userID,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateIssueView(w http.ResponseWriter, r *http.Request) {
	view, member, ok := h.loadIssueViewForUser(w, r)
	if !ok {
		return
	}
	if !issueViewCanEdit(view, member) {
		writeError(w, http.StatusForbidden, "only the view creator or a workspace admin can edit this view")
		return
	}
	var req issueViewUpdateRequest
	if !decodeIssueViewJSON(w, r, &req) {
		return
	}
	name := view.Name
	if req.Name != nil {
		var valid bool
		name, valid = validateIssueViewName(*req.Name)
		if !valid {
			writeError(w, http.StatusBadRequest, "name must be between 1 and 80 characters")
			return
		}
	}
	icon := textToPtr(view.Icon)
	if req.Icon.Set {
		icon = req.Icon.Value
	}
	color := textToPtr(view.Color)
	if req.Color.Set {
		color = req.Color.Value
	}
	if !validateIssueViewOptionalText(icon, 64) || !validateIssueViewOptionalText(color, 32) {
		writeError(w, http.StatusBadRequest, "icon or color is too long")
		return
	}
	visibility := view.Visibility
	if req.Visibility != nil {
		visibility = *req.Visibility
	}
	if !validateIssueViewVisibility(visibility) {
		writeError(w, http.StatusBadRequest, "visibility must be 'private' or 'workspace'")
		return
	}
	if view.ScopeType == "my" && visibility != "private" {
		writeError(w, http.StatusBadRequest, "My Issues views must be private")
		return
	}
	definition := view.Definition
	if len(bytes.TrimSpace(req.Definition)) > 0 {
		var err error
		definition, err = validateIssueViewDefinition(req.Definition)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	updated, err := h.Queries.UpdateIssueView(r.Context(), db.UpdateIssueViewParams{
		Name: name, Icon: textFromPtr(icon), Color: textFromPtr(color),
		Visibility: visibility, Definition: definition, ID: view.ID,
		WorkspaceID: view.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update view")
		return
	}
	resp := issueViewToResponse(updated, member)
	userID := uuidToString(member.UserID)
	h.publish(protocol.EventIssueViewUpdated, uuidToString(view.WorkspaceID), "member", userID, map[string]any{
		"view_id": uuidToString(updated.ID), "visibility": updated.Visibility,
		"previous_visibility": view.Visibility,
		"recipient_id":        uuidToString(updated.CreatorID),
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteIssueView(w http.ResponseWriter, r *http.Request) {
	view, member, ok := h.loadIssueViewForUser(w, r)
	if !ok {
		return
	}
	if !issueViewCanEdit(view, member) {
		writeError(w, http.StatusForbidden, "only the view creator or a workspace admin can delete this view")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteIssueViewPreferencesByView(r.Context(), db.DeleteIssueViewPreferencesByViewParams{
		WorkspaceID: view.WorkspaceID, DefaultViewID: view.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	if err := qtx.DeletePinnedItemsByItem(r.Context(), db.DeletePinnedItemsByItemParams{
		ItemType: "view", ItemID: view.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	if err := qtx.DeleteIssueView(r.Context(), db.DeleteIssueViewParams{
		ID: view.ID, WorkspaceID: view.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete view")
		return
	}
	workspaceID := uuidToString(view.WorkspaceID)
	userID := uuidToString(member.UserID)
	h.publish(protocol.EventIssueViewDeleted, workspaceID, "member", userID, map[string]any{
		"view_id": uuidToString(view.ID), "visibility": view.Visibility,
		"recipient_id": uuidToString(view.CreatorID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DuplicateIssueView(w http.ResponseWriter, r *http.Request) {
	view, member, ok := h.loadIssueViewForUser(w, r)
	if !ok {
		return
	}
	var req issueViewDuplicateRequest
	if !decodeIssueViewJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = view.Name + " copy"
	}
	name, valid := validateIssueViewName(req.Name)
	if !valid {
		writeError(w, http.StatusBadRequest, "name must be between 1 and 80 characters")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if !validateIssueViewVisibility(req.Visibility) {
		writeError(w, http.StatusBadRequest, "visibility must be 'private' or 'workspace'")
		return
	}
	if view.ScopeType == "my" {
		req.Visibility = "private"
	}
	scope := issueViewScope{typeName: view.ScopeType, dbScopeID: view.ScopeID}
	if view.ScopeType == "workspace" {
		scope.preferenceScopeID = view.WorkspaceID
	} else if view.ScopeType == "my" {
		scope.preferenceScopeID = member.UserID
	} else {
		scope.preferenceScopeID = view.ScopeID
	}
	duplicate, err := h.createIssueViewRow(
		r.Context(), view.WorkspaceID, member.UserID, name,
		textToPtr(view.Icon), textToPtr(view.Color), scope, req.Visibility, view.Definition,
	)
	if err != nil {
		if err.Error() == "view limit reached" {
			writeError(w, http.StatusConflict, "a workspace cannot have more than 100 views")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace or project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to duplicate view")
		return
	}
	resp := issueViewToResponse(duplicate, member)
	workspaceID := uuidToString(view.WorkspaceID)
	userID := uuidToString(member.UserID)
	h.publish(protocol.EventIssueViewCreated, workspaceID, "member", userID, map[string]any{
		"view_id": uuidToString(duplicate.ID), "visibility": duplicate.Visibility,
		"recipient_id": userID,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) SetDefaultIssueView(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	var req issueViewDefaultRequest
	if !decodeIssueViewJSON(w, r, &req) {
		return
	}
	scope, ok := h.resolveIssueViewScope(w, r, workspaceID, userID, req.ScopeType, req.ScopeID)
	if !ok {
		return
	}
	params := db.ClearDefaultIssueViewParams{
		WorkspaceID: parseUUID(workspaceID), UserID: parseUUID(userID),
		ScopeType: scope.typeName, ScopeID: scope.preferenceScopeID,
	}
	if req.ViewID == nil || strings.TrimSpace(*req.ViewID) == "" {
		if err := h.Queries.ClearDefaultIssueView(r.Context(), params); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear default view")
			return
		}
	} else {
		viewID, ok := parseUUIDOrBadRequest(w, *req.ViewID, "view_id")
		if !ok {
			return
		}
		view, err := h.Queries.GetIssueViewForUser(r.Context(), db.GetIssueViewForUserParams{
			ID: viewID, WorkspaceID: parseUUID(workspaceID), UserID: parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "view not found")
			return
		}
		if view.ScopeType != scope.typeName || view.ScopeID != scope.dbScopeID {
			writeError(w, http.StatusBadRequest, "view belongs to a different surface")
			return
		}
		if err := h.Queries.SetDefaultIssueView(r.Context(), db.SetDefaultIssueViewParams{
			WorkspaceID: params.WorkspaceID, UserID: params.UserID,
			ScopeType: params.ScopeType, ScopeID: params.ScopeID, DefaultViewID: view.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set default view")
			return
		}
	}
	h.publish(protocol.EventIssueViewDefaultChanged, workspaceID, "member", userID, map[string]any{
		"scope_type": scope.typeName, "scope_id": req.ScopeID, "view_id": req.ViewID,
		"recipient_id": userID,
	})
	w.WriteHeader(http.StatusNoContent)
}

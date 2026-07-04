package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type IssueTypeResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Icon        string  `json:"icon"`
	Color       string  `json:"color"`
	IsDefault   bool    `json:"is_default"`
	Position    int32   `json:"position"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func issueTypeToResponse(i db.IssueType) IssueTypeResponse {
	var desc *string
	if i.Description.Valid {
		desc = &i.Description.String
	}
	return IssueTypeResponse{
		ID:          i.ID.String(),
		WorkspaceID: i.WorkspaceID.String(),
		Name:        i.Name,
		Description: desc,
		Icon:        i.Icon,
		Color:       i.Color,
		IsDefault:   i.IsDefault,
		Position:    i.Position,
		CreatedAt:   timestampToString(i.CreatedAt),
		UpdatedAt:   timestampToString(i.UpdatedAt),
	}
}

func (h *Handler) ListIssueTypes(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	issueTypes, err := h.Queries.ListIssueTypes(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue types")
		return
	}

	res := make([]IssueTypeResponse, len(issueTypes))
	for i, it := range issueTypes {
		res[i] = issueTypeToResponse(it)
	}

	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) GetIssueType(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueTypeId"), "issueTypeId")
	if !ok {
		return
	}

	it, err := h.Queries.GetIssueType(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue type not found")
		return
	}

	if it.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, issueTypeToResponse(it))
}

func (h *Handler) CreateIssueType(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	var body struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Icon        string  `json:"icon"`
		Color       string  `json:"color"`
		IsDefault   bool    `json:"is_default"`
		Position    int32   `json:"position"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var desc pgtype.Text
	if body.Description != nil {
		desc = pgtype.Text{String: *body.Description, Valid: true}
	}

	if body.Icon == "" {
		body.Icon = "circle"
	}
	if body.Color == "" {
		body.Color = "#6B7280"
	}

	it, err := h.Queries.CreateIssueType(r.Context(), db.CreateIssueTypeParams{
		WorkspaceID: wsID,
		Name:        body.Name,
		Description: desc,
		Icon:        body.Icon,
		Color:       body.Color,
		IsDefault:   body.IsDefault,
		Position:    body.Position,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue type")
		return
	}

	res := issueTypeToResponse(it)
	h.publish(protocol.EventIssueTypeCreated, wsIDStr, "system", "", res)

	writeJSON(w, http.StatusCreated, res)
}

func (h *Handler) UpdateIssueType(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueTypeId"), "issueTypeId")
	if !ok {
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Icon        *string `json:"icon"`
		Color       *string `json:"color"`
		IsDefault   *bool   `json:"is_default"`
		Position    *int32  `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	existing, err := h.Queries.GetIssueType(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue type not found")
		return
	}
	if existing.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	arg := db.UpdateIssueTypeParams{
		ID: id,
	}
	if body.Name != nil {
		arg.Name = pgtype.Text{String: *body.Name, Valid: true}
	}
	if body.Description != nil {
		arg.Description = pgtype.Text{String: *body.Description, Valid: true}
	}
	if body.Icon != nil {
		arg.Icon = pgtype.Text{String: *body.Icon, Valid: true}
	}
	if body.Color != nil {
		arg.Color = pgtype.Text{String: *body.Color, Valid: true}
	}
	if body.IsDefault != nil {
		arg.IsDefault = pgtype.Bool{Bool: *body.IsDefault, Valid: true}
	}
	if body.Position != nil {
		arg.Position = pgtype.Int4{Int32: *body.Position, Valid: true}
	}

	it, err := h.Queries.UpdateIssueType(r.Context(), arg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue type")
		return
	}

	res := issueTypeToResponse(it)
	h.publish(protocol.EventIssueTypeUpdated, wsIDStr, "system", "", res)

	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) DeleteIssueType(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueTypeId"), "issueTypeId")
	if !ok {
		return
	}

	it, err := h.Queries.GetIssueType(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue type not found")
		return
	}

	if it.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.Queries.DeleteIssueType(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue type")
		return
	}

	h.publish(protocol.EventIssueTypeDeleted, wsIDStr, "system", "", map[string]string{"id": id.String()})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) SeedDefaultIssueTypes(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	if err := h.Queries.SeedDefaultIssueTypes(r.Context(), wsID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed issue types")
		return
	}

	// Just return all issue types after seeding
	h.ListIssueTypes(w, r)
}

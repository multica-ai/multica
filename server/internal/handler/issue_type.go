package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type IssueTypeResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Color       string  `json:"color"`
	Icon        string  `json:"icon"`
	LoadProfile string  `json:"load_profile"`
	IsSystem    bool    `json:"is_system"`
	ArchivedAt  *string `json:"archived_at"`
	Position    int32   `json:"position"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type createIssueTypeRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	LoadProfile string `json:"load_profile"`
	Position    int32  `json:"position"`
}

type updateIssueTypeRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	LoadProfile *string `json:"load_profile"`
	Position    *int32  `json:"position"`
}

func issueTypeToResponse(issueType db.IssueType) IssueTypeResponse {
	return IssueTypeResponse{
		ID:          uuidToString(issueType.ID),
		WorkspaceID: uuidToString(issueType.WorkspaceID),
		Key:         issueType.Key,
		Name:        issueType.Name,
		Description: issueType.Description,
		Color:       issueType.Color,
		Icon:        issueType.Icon,
		LoadProfile: issueType.LoadProfile,
		IsSystem:    issueType.IsSystem,
		ArchivedAt:  timestampToPtr(issueType.ArchivedAt),
		Position:    issueType.Position,
		CreatedAt:   timestampToString(issueType.CreatedAt),
		UpdatedAt:   timestampToString(issueType.UpdatedAt),
	}
}

func validIssueTypeLoadProfile(value string) bool {
	switch value {
	case "deep_work", "light_work", "recovery", "neutral":
		return true
	default:
		return false
	}
}

func normalizeIssueTypeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, " ", "-")
	return key
}

func (h *Handler) ListIssueTypes(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	workspaceUUID := parseUUID(workspaceID)
	if err := h.Queries.EnsureDefaultIssueTypes(r.Context(), workspaceUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue types")
		return
	}
	types, err := h.Queries.ListIssueTypes(r.Context(), db.ListIssueTypesParams{
		WorkspaceID:     workspaceUUID,
		IncludeArchived: pgtype.Bool{Bool: strings.EqualFold(r.URL.Query().Get("include_archived"), "true"), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue types")
		return
	}
	resp := make([]IssueTypeResponse, len(types))
	for i, issueType := range types {
		resp[i] = issueTypeToResponse(issueType)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateIssueType(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	var req createIssueTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Key = normalizeIssueTypeKey(req.Key)
	if req.Key == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "key and name are required")
		return
	}
	if req.Color == "" {
		req.Color = "gray"
	}
	if req.Icon == "" {
		req.Icon = "circle"
	}
	if req.LoadProfile == "" {
		req.LoadProfile = "neutral"
	}
	if !validIssueTypeLoadProfile(req.LoadProfile) {
		writeError(w, http.StatusBadRequest, "invalid load_profile")
		return
	}
	issueType, err := h.Queries.CreateIssueType(r.Context(), db.CreateIssueTypeParams{
		WorkspaceID: parseUUID(workspaceID),
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
		Icon:        req.Icon,
		LoadProfile: req.LoadProfile,
		Position:    req.Position,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "issue type key already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create issue type")
		return
	}
	writeJSON(w, http.StatusCreated, issueTypeToResponse(issueType))
}

func (h *Handler) UpdateIssueType(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	var req updateIssueTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LoadProfile != nil && !validIssueTypeLoadProfile(*req.LoadProfile) {
		writeError(w, http.StatusBadRequest, "invalid load_profile")
		return
	}
	var position pgtype.Int4
	if req.Position != nil {
		position = pgtype.Int4{Int32: *req.Position, Valid: true}
	}
	issueType, err := h.Queries.UpdateIssueType(r.Context(), db.UpdateIssueTypeParams{
		ID:          parseUUID(chi.URLParam(r, "id")),
		WorkspaceID: parseUUID(workspaceID),
		Name:        ptrToText(req.Name),
		Description: ptrToText(req.Description),
		Color:       ptrToText(req.Color),
		Icon:        ptrToText(req.Icon),
		LoadProfile: ptrToText(req.LoadProfile),
		Position:    position,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update issue type")
		return
	}
	writeJSON(w, http.StatusOK, issueTypeToResponse(issueType))
}

func (h *Handler) ArchiveIssueType(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	issueType, err := h.Queries.ArchiveIssueType(r.Context(), db.ArchiveIssueTypeParams{
		ID:          parseUUID(chi.URLParam(r, "id")),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to archive issue type")
		return
	}
	writeJSON(w, http.StatusOK, issueTypeToResponse(issueType))
}

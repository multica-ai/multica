package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-user view-bar preferences (hidden items + order) for one surface
// container. The prefs document is client-owned and opaque here beyond
// being a JSON object; item ids inside are "builtin:<key>" / "view:<uuid>".

// resolvePreferenceScope validates scope params and backfills scope_id so
// the composite key never carries NULL: workspace scope uses the workspace
// id, my scope the user id, project scope the project id (validated).
func (h *Handler) resolvePreferenceScope(
	w http.ResponseWriter, r *http.Request,
	wsUUID pgtype.UUID, userID string, scopeType, rawScopeID string,
) (pgtype.UUID, bool) {
	switch scopeType {
	case "workspace":
		return wsUUID, true
	case "my":
		return parseUUID(userID), true
	case "project":
		if rawScopeID == "" {
			writeError(w, http.StatusBadRequest, "scope_id is required for project scope")
			return pgtype.UUID{}, false
		}
		projUUID, ok := parseUUIDOrBadRequest(w, rawScopeID, "scope_id")
		if !ok {
			return pgtype.UUID{}, false
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: projUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return pgtype.UUID{}, false
		}
		return projUUID, true
	default:
		writeError(w, http.StatusBadRequest, "invalid scope_type")
		return pgtype.UUID{}, false
	}
}

type IssueViewPreferenceResponse struct {
	ScopeType string          `json:"scope_type"`
	ScopeID   *string         `json:"scope_id"`
	Prefs     json.RawMessage `json:"prefs"`
	UpdatedAt string          `json:"updated_at"`
}

func (h *Handler) GetIssueViewPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	scopeType := r.URL.Query().Get("scope_type")
	scopeID, ok := h.resolvePreferenceScope(w, r, wsUUID, userID, scopeType, r.URL.Query().Get("scope_id"))
	if !ok {
		return
	}

	pref, err := h.Queries.GetIssueViewPreference(r.Context(), db.GetIssueViewPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		ScopeType:   scopeType,
		ScopeID:     scopeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No row yet — an empty document, not an error.
			writeJSON(w, http.StatusOK, IssueViewPreferenceResponse{
				ScopeType: scopeType,
				ScopeID:   uuidToPtr(scopeID),
				Prefs:     json.RawMessage("{}"),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load preference")
		return
	}
	writeJSON(w, http.StatusOK, IssueViewPreferenceResponse{
		ScopeType: pref.ScopeType,
		ScopeID:   uuidToPtr(pref.ScopeID),
		Prefs:     json.RawMessage(pref.Prefs),
		UpdatedAt: timestampToString(pref.UpdatedAt),
	})
}

type PutIssueViewPreferenceRequest struct {
	ScopeType string          `json:"scope_type"`
	ScopeID   *string         `json:"scope_id"`
	Prefs     json.RawMessage `json:"prefs"`
}

func (h *Handler) PutIssueViewPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	var req PutIssueViewPreferenceRequest
	r.Body = http.MaxBytesReader(w, r.Body, issueViewBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rawScopeID := ""
	if req.ScopeID != nil {
		rawScopeID = *req.ScopeID
	}
	scopeID, ok := h.resolvePreferenceScope(w, r, wsUUID, userID, req.ScopeType, rawScopeID)
	if !ok {
		return
	}
	if len(req.Prefs) == 0 {
		req.Prefs = json.RawMessage("{}")
	}
	if !isJSONObject(req.Prefs) {
		writeError(w, http.StatusBadRequest, "prefs must be a JSON object")
		return
	}

	pref, err := h.Queries.UpsertIssueViewPreference(r.Context(), db.UpsertIssueViewPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		ScopeType:   req.ScopeType,
		ScopeID:     scopeID,
		Prefs:       req.Prefs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save preference")
		return
	}
	writeJSON(w, http.StatusOK, IssueViewPreferenceResponse{
		ScopeType: pref.ScopeType,
		ScopeID:   uuidToPtr(pref.ScopeID),
		Prefs:     json.RawMessage(pref.Prefs),
		UpdatedAt: timestampToString(pref.UpdatedAt),
	})
}

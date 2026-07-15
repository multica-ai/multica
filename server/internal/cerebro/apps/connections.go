package apps

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
)

// CallConnection lets published app code call exactly one configured endpoint.
// The connection credential stays in Multica; the app receives only the result.
func (h *Handler) CallConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	connectionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid connection id")
		return
	}
	var req connectionCallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	appID, err := uuid.Parse(req.AppID)
	if err != nil || !semverPattern.MatchString(req.Version) || strings.TrimSpace(req.Tool) == "" {
		writeError(w, http.StatusBadRequest, "app_id, version, and tool are required")
		return
	}
	var rawScopes json.RawMessage
	err = h.pool.QueryRow(r.Context(), `
		SELECT g.scopes FROM cerebro_app a
		JOIN cerebro_app_grant g ON g.app_id=a.id AND g.version=$3 AND g.status='approved'
		WHERE a.id=$1 AND a.workspace_id=$2 AND a.current_version=$3 AND a.status='published'`, appID, workspaceID, req.Version).Scan(&rawScopes)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusForbidden, "app is not published with approved scopes")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize app connection")
		return
	}
	var scopes []tokens.Scope
	if json.Unmarshal(rawScopes, &scopes) != nil || !approvedConnectionScope(scopes, connectionID.String()) {
		writeError(w, http.StatusForbidden, "connection is outside the app's approved scopes")
		return
	}
	if h.connections == nil {
		writeError(w, http.StatusServiceUnavailable, "connection service is unavailable")
		return
	}
	result, err := h.connections.CallForApp(r.Context(), workspaceID, memberID, connectionID, req.Tool, req.Arguments)
	if err != nil {
		writeError(w, http.StatusFailedDependency, "connection call failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

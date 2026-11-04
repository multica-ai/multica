package handler

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
)

// GetCompanyOrgMap returns the org map of the workspace. Any member may read it.
// An optional floor_id narrows the floors and squads to one floor.
//
//	GET /api/company/org-map[?floor_id=<uuid>]
func (h *Handler) GetCompanyOrgMap(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var floorID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("floor_id")); raw != "" {
		floorID, ok = parseUUIDOrBadRequest(w, raw, "floor_id")
		if !ok {
			return
		}
	}
	out, err := service.LoadOrgMap(r.Context(), h.Queries, wsUUID, floorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the org map")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

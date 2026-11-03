package handler

import (
	"net/http"
)

// SquadDirectoryEntryResponse is one squad as another squad reads it before it
// asks for work (spec 3.5): what the squad takes on, how fast it answers, and how
// many requests are open with it.
type SquadDirectoryEntryResponse struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	FloorID       *string `json:"floor_id"`
	FloorName     string  `json:"floor_name"`
	Mission       string  `json:"mission"`
	Intake        string  `json:"intake"`
	ResponseHours int     `json:"response_hours"`
	OpenRequests  int64   `json:"open_requests"`
}

// ListSquadDirectory returns the live squads of the workspace with their intake
// and response time. Any member may read it.
func (h *Handler) ListSquadDirectory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListSquadDirectory(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the squad directory")
		return
	}
	out := make([]SquadDirectoryEntryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, SquadDirectoryEntryResponse{
			ID:            uuidToString(row.SquadID),
			Name:          row.Name,
			FloorID:       uuidToPtr(row.FloorID),
			FloorName:     row.FloorName,
			Mission:       row.Mission,
			Intake:        row.Intake,
			ResponseHours: int(row.ResponseHours),
			OpenRequests:  row.OpenRequests,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"squads": out})
}

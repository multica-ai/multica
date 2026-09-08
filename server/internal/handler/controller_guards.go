package handler

import (
	"encoding/json"
	"net/http"
)

func controllerProtectedFields(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"status", "assignee_id", "assignee_type", "project_id"} {
		if _, exists := fields[key]; exists {
			return true
		}
	}
	return false
}

func controllerProtectedMetadata(key string) bool {
	switch key {
	case "controller_revision", "controller_checked_at", "controller_state", "last_progress_what", "last_progress_at", "owner_action", "delivery_state":
		return true
	}
	return false
}

func (h *Handler) controllerOwnsProperty(w http.ResponseWriter, r *http.Request, issueID, propertyID string) bool {
	if h.DB == nil {
		writeError(w, 503, "controller store unavailable")
		return true
	}
	var protected bool
	err := h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM issue_controller WHERE issue_id=$1 AND config->'protected_property_ids' ? $2)`, parseUUID(issueID), propertyID).Scan(&protected)
	if err != nil {
		writeError(w, 503, "controller authority unavailable")
		return true
	}
	if protected {
		writeError(w, 403, "this property is controller owned")
		return true
	}
	return false
}

package handler

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type UpdateSystemSettingsRequest struct {
	Settings []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"settings"`
}

func (h *Handler) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireSystemAdmin(w, r)
	if !ok {
		return
	}

	var req UpdateSystemSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, s := range req.Settings {
		err := h.Queries.UpdateSystemSetting(r.Context(), db.UpdateSystemSettingParams{
			Key:   s.Key,
			Value: s.Value,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update setting: "+s.Key)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

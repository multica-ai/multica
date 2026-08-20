package handler

import "net/http"

func (h *Handler) GetCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"comment_branch_v1": true,
	})
}

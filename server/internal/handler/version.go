package handler

import "net/http"

type ServerVersionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	version := h.cfg.ServerVersion
	if version == "" {
		version = "dev"
	}
	commit := h.cfg.ServerCommit
	if commit == "" {
		commit = "unknown"
	}
	writeJSON(w, http.StatusOK, ServerVersionResponse{
		Version: version,
		Commit:  commit,
	})
}

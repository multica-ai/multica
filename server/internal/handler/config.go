package handler

import "net/http"

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}
	writeJSON(w, http.StatusOK, config)
}

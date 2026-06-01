package main

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

type adminListEntry struct {
	WorkspaceID string `json:"workspace_id"`
	URL         string `json:"url"`
	BarePath    string `json:"bare_path"`
}

// NewAdminMux returns the stdlib mux serving the admin API. We intentionally
// don't bring chi into this binary — three routes don't justify a router dep.
//
// Routes:
//   - GET  /healthz                                         → "ok"
//   - GET  /repos                                           → JSON list of mirrored repos (currently empty;
//                                                              Cache doesn't yet expose List())
//   - POST /repos/fetch?workspace_id=...&url=...            → force a fetch on one repo
func NewAdminMux(cache *repocache.Cache, _ *Config) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/repos", func(w http.ResponseWriter, r *http.Request) {
		// Cache doesn't expose List() today; return an empty array. Operator
		// inspection of /repos contents goes through `kubectl exec ... ls /repos`.
		entries := []adminListEntry{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})

	mux.HandleFunc("/repos/fetch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws := r.URL.Query().Get("workspace_id")
		url := r.URL.Query().Get("url")
		if ws == "" || url == "" {
			http.Error(w, "workspace_id and url required", http.StatusBadRequest)
			return
		}
		bare := cache.Lookup(ws, url)
		if bare == "" {
			http.Error(w, "unknown", http.StatusNotFound)
			return
		}
		if err := cache.Fetch(bare); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("fetched\n"))
	})

	return mux
}

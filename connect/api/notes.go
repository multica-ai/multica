package api

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica-connect/store"
)

// NotesAPI exposes note store endpoints for the sync daemon.
type NotesAPI struct {
	Store *store.NoteStore
}

// RegisterRoutes adds note endpoints to a mux.
func (a *NotesAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notes/unsynced", a.listUnsynced)
	mux.HandleFunc("POST /api/notes/{id}/synced", a.markSynced)
}

func (a *NotesAPI) listUnsynced(w http.ResponseWriter, r *http.Request) {
	notes, err := a.Store.ListUnsynced()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []store.Note{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"notes": notes})
}

func (a *NotesAPI) markSynced(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := a.Store.MarkSynced(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

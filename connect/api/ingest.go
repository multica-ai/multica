package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/handler"
	"github.com/multica-ai/multica-connect/triage"
)

var urlRegex = regexp.MustCompile(`https?://[^\s<>"']+`)

func extractURLs(text string) []string {
	return urlRegex.FindAllString(text, -1)
}

// IngestRequest is the payload from iPhone Shortcuts or other HTTP clients.
type IngestRequest struct {
	Text string `json:"text"`
}

// IngestAPI accepts messages directly via HTTP (bypasses Telegram).
type IngestAPI struct {
	Classifier *triage.Classifier
	Router     *handler.Router
}

func (a *IngestAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ingest", a.handle)
}

func (a *IngestAPI) handle(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	msg := channel.Message{
		Source:   "shortcut",
		ThreadID: "shortcut",
		UserName: "iPhone",
		Text:     req.Text,
		URLs:     extractURLs(req.Text),
	}

	result, err := a.Classifier.Classify(ctx, req.Text)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp, err := a.Router.Route(ctx, msg, result)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"category": result.Category,
		"title":    result.Title,
		"response": resp.Text,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}


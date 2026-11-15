package handler

import (
	"log/slog"
	"net/http"
)

// CreateVoiceSession mints a short-lived token for one voice-input stream
// from the web app. The provider's API key stays on the server; the token
// only has to be presented when the browser opens the stream, which then
// runs until the browser closes it. Returns 404 when the operator has not
// configured a speech provider; /api/config then omits
// voice_transcription_enabled and the web app shows no mic.
func (h *Handler) CreateVoiceSession(w http.ResponseWriter, r *http.Request) {
	if h.Speech == nil {
		writeError(w, http.StatusNotFound, "voice input is not configured on this server")
		return
	}
	session, err := h.Speech.NewSession(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "voice input: speech provider refused a session", "error", err)
		writeError(w, http.StatusBadGateway, "the speech provider could not be reached")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, session)
}

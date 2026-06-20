package dictation

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	// A dictation utterance is seconds of Opus/WebM — kilobytes. The cap only
	// guards against a runaway upload, never a legitimate clip.
	maxUploadBytes    = 25 << 20 // 25 MB
	transcribeTimeout = 120 * time.Second
)

// transcribeResponse is the shape the cerebro-inference hviske endpoint returns
// and the shape we hand back to the browser.
type transcribeResponse struct {
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

// Transcribe is the one-shot HTTP dictation path: the browser records an
// utterance, POSTs it as multipart `file`, and we forward it to the
// cerebro-inference hviske endpoint (carrying the inference bearer key the
// agent never sees) and return the transcribed text. This mirrors the auth +
// membership gate of Stream; it is the non-streaming sibling that the chat,
// notes, and document editors use today.
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.workspaceIDFromRequest(r)
	if workspaceID == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Dictation requires a workspace.")
		return
	}
	userID, err := h.userIDFromRequest(r)
	if err != nil || userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Dictation requires an authenticated workspace member.")
		return
	}
	if !h.isWorkspaceMember(r.Context(), userID, workspaceID) {
		writeJSONError(w, http.StatusForbidden, "unauthorized", "Dictation requires an authenticated workspace member.")
		return
	}
	if h.transcribeURL == "" {
		writeJSONError(w, http.StatusServiceUnavailable, "backend_not_configured", "Dictation transcription backend is not configured.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Expected a multipart 'file' field with the audio clip.")
		return
	}
	defer file.Close()

	upstreamReq, err := h.buildTranscribeRequest(r, file, header, workspaceID, userID)
	if err != nil {
		slog.Warn("dictation transcribe request build failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Could not prepare the transcription request.")
		return
	}

	resp, err := h.httpClient.Do(upstreamReq)
	if err != nil {
		slog.Warn("dictation transcribe upstream failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "backend_unavailable", "Dictation transcription backend is unavailable.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("dictation transcribe upstream non-200", "status", resp.StatusCode)
		writeJSONError(w, http.StatusBadGateway, "backend_error", "Dictation transcription backend returned an error.")
		return
	}

	var parsed transcribeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		slog.Warn("dictation transcribe decode failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "backend_error", "Dictation transcription backend returned an unreadable response.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(parsed)
}

// buildTranscribeRequest re-wraps the uploaded clip as a fresh multipart body
// and points it at the inference endpoint with the bearer key.
func (h *Handler) buildTranscribeRequest(
	r *http.Request,
	file io.Reader,
	header *multipart.FileHeader,
	workspaceID, userID string,
) (*http.Request, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	filename := "audio"
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	// Pass the language hint through if the browser sent one.
	if lang := r.FormValue("language"); lang != "" {
		_ = mw.WriteField("language", lang)
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.transcribeURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if h.inferenceKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.inferenceKey)
	}
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("X-User-ID", userID)
	return req, nil
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(outboundError{
		Type:    "error",
		Code:    code,
		Message: message,
	})
}

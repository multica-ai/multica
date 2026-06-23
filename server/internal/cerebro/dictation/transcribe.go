package dictation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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

	// Cold-start handling (Jesper, FIR-1637). The hviske GPU on Modal scales to
	// zero when idle (scaledown_window=300s), so the first dictation after a
	// pause cold-boots the container and primes CUDA. That first inference can
	// fail with a 5xx (or the connection can drop mid-boot), and then the
	// now-warm container serves the very next request in ~1.5s. Without a retry
	// that cold start surfaced to the user as "failed". We retry the upstream a
	// few times so a cold start is invisible instead of a hard failure.
	transcribeMaxAttempts = 3
	transcribeRetryDelay  = 750 * time.Millisecond

	// Warmup runs decoupled from the browser request (fire-and-forget), so it
	// gets its own budget rather than the caller's context.
	warmupTimeout = 90 * time.Second
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

	filename := "audio"
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}
	// FIR-1797: bias decoding toward this workspace's proper nouns and run the
	// optional cleanup pass. The glossary is built automatically from the
	// workspace's business objects (agents, squads, projects, teammates) and the
	// user's manual terms are merged on top. Both glossary and cleanup are
	// advisory — the inference service ignores an empty glossary and only cleans
	// up when its own key is configured.
	glossary := mergeGlossary(r.FormValue("glossary"), h.workspaceGlossary(r.Context(), workspaceID))
	body, contentType, err := buildMultipart(file, filename, r.FormValue("language"), glossary, r.FormValue("cleanup"))
	if err != nil {
		slog.Warn("dictation transcribe request build failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Could not prepare the transcription request.")
		return
	}

	parsed, err := h.transcribeWithRetry(r.Context(), body, contentType, workspaceID, userID)
	if err != nil {
		slog.Warn("dictation transcribe upstream failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "backend_unavailable", "Dictation transcription backend is unavailable.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(parsed)
}

// Warmup nudges the hviske engine awake without the user waiting on it. The
// browser fires this the moment the mic is pressed (Jesper, FIR-1637): the
// engine cold-boots and primes CUDA while the user is still speaking, so by the
// time they release the mic the real transcribe lands on a warm container in
// ~1.5s instead of paying the full cold start. It is best-effort — the browser
// ignores the result, and the transcribe retry is the real safety net — so we
// always answer 202 and never surface an error to the caller.
func (h *Handler) Warmup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.workspaceIDFromRequest(r)
	userID, err := h.userIDFromRequest(r)
	if workspaceID == "" || err != nil || userID == "" || !h.isWorkspaceMember(r.Context(), userID, workspaceID) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Dictation requires an authenticated workspace member.")
		return
	}
	if h.transcribeURL != "" {
		// Decoupled from the request lifecycle: a quick unmount or navigation
		// must not cancel the boot we just paid to start.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), warmupTimeout)
			defer cancel()
			body, contentType, err := buildMultipart(bytes.NewReader(silentWAV()), "warmup.wav", "", "", "")
			if err != nil {
				return
			}
			// A single best-effort attempt is enough to boot the container; the
			// transcribe path retries for the actual clip.
			if _, err := h.transcribeOnce(ctx, body, contentType, workspaceID, userID); err != nil {
				slog.Debug("dictation warmup ping failed (non-fatal)", "error", err)
			}
		}()
	}

	// Always a no-op success — the client never blocks on or reacts to warmup.
	w.WriteHeader(http.StatusAccepted)
}

// transcribeWithRetry forwards the clip to the hviske endpoint, retrying a cold
// start (5xx or a dropped connection) so the user sees text rather than
// "failed". 4xx responses are deterministic and never retried.
func (h *Handler) transcribeWithRetry(
	ctx context.Context,
	body []byte,
	contentType, workspaceID, userID string,
) (*transcribeResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= transcribeMaxAttempts; attempt++ {
		parsed, err := h.transcribeOnce(ctx, body, contentType, workspaceID, userID)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
		if !errRetryable(err) {
			return nil, err
		}
		if attempt < transcribeMaxAttempts {
			slog.Info("dictation transcribe retrying after cold-start failure", "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(transcribeRetryDelay):
			}
		}
	}
	return nil, lastErr
}

// upstreamStatusError carries the upstream HTTP status so the retry layer can
// tell a retryable cold-start 5xx from a deterministic 4xx.
type upstreamStatusError struct{ status int }

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("hviske upstream returned %d", e.status)
}

// errRetryable reports whether a failed attempt is worth retrying: a transport
// error (connection dropped during cold boot) or an upstream 5xx. A 4xx is the
// caller's fault and will never change on retry.
func errRetryable(err error) bool {
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status >= 500
	}
	// Transport-level error (no response): retryable.
	return true
}

// transcribeOnce performs a single upstream call, returning the parsed text on
// 200 and an error (typed for a non-200 status, transport for connection
// failures) otherwise.
func (h *Handler) transcribeOnce(
	ctx context.Context,
	body []byte,
	contentType, workspaceID, userID string,
) (*transcribeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.transcribeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if h.inferenceKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.inferenceKey)
	}
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("X-User-ID", userID)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, &upstreamStatusError{status: resp.StatusCode}
	}

	var parsed transcribeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode hviske response: %w", err)
	}
	return &parsed, nil
}

// buildMultipart wraps a clip as a multipart `file` field (plus optional
// language hint, glossary bias, and cleanup flag) and returns the encoded body
// and its content type, so the retry layer can replay the exact bytes on every
// attempt.
func buildMultipart(file io.Reader, filename, language, glossary, cleanup string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}
	if language != "" {
		_ = mw.WriteField("language", language)
	}
	if glossary != "" {
		_ = mw.WriteField("glossary", glossary)
	}
	if cleanup != "" {
		_ = mw.WriteField("cleanup", cleanup)
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// silentWAV returns a tiny (~100ms) 16 kHz mono 16-bit PCM clip of silence.
// It carries no speech — its only job is to make the warmup ping a real decode
// so the engine boots and primes its audio pipeline.
func silentWAV() []byte {
	const (
		sampleRate    = 16000
		numSamples    = sampleRate / 10 // 100ms
		bytesPerSmpl  = 2
		dataLen       = numSamples * bytesPerSmpl
		headerLen     = 44
		byteRate      = sampleRate * bytesPerSmpl
		bitsPerSample = 16
	)
	buf := make([]byte, headerLen+dataLen)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:24], 1)  // mono
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], byteRate)
	binary.LittleEndian.PutUint16(buf[32:34], bytesPerSmpl)
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))
	// Sample bytes stay zero — silence.
	return buf
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

package handler

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/llm"
)

const (
	// maxVoiceClipSize caps one recording. Opus at the browser's default
	// bitrate stays well under this for the two minutes the client allows.
	maxVoiceClipSize = 10 << 20
	// maxVoicePromptRunes bounds the vocabulary hint; transcription models
	// read only the first couple of hundred tokens of it anyway.
	maxVoicePromptRunes    = 600
	voiceTranscribeTimeout = 60 * time.Second
)

var voiceLanguagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)

// voiceAudioExtensions maps the container types browsers record into to the
// file extension the upstream uses to pick a decoder. The client's filename is
// never forwarded.
var voiceAudioExtensions = map[string]string{
	"audio/webm":  "webm",
	"video/webm":  "webm",
	"audio/ogg":   "ogg",
	"audio/mp4":   "mp4",
	"audio/x-m4a": "m4a",
	"audio/mpeg":  "mp3",
	"audio/wav":   "wav",
	"audio/x-wav": "wav",
}

type voiceTranscriptionResponse struct {
	Text string `json:"text"`
}

// voiceTranscriptionAvailable reports whether this deployment can turn speech
// into text: the assist layer is configured and the operator named a
// transcription model.
func (h *Handler) voiceTranscriptionAvailable() bool {
	return h.LLM.Enabled() && strings.TrimSpace(h.cfg.LLMTranscribeModel) != ""
}

// TranscribeVoice turns one recorded clip into text for a composer. The audio
// is streamed to the configured upstream and never stored.
func (h *Handler) TranscribeVoice(w http.ResponseWriter, r *http.Request) {
	if !h.voiceTranscriptionAvailable() {
		writeFeatureDisabled(w, "voice_input_not_configured", "voice input is not configured on this server")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceClipSize+(64<<10))
	if err := r.ParseMultipartForm(maxVoiceClipSize); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "recording is too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid multipart form")
		}
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	if header.Size > maxVoiceClipSize {
		writeError(w, http.StatusRequestEntityTooLarge, "recording is too large")
		return
	}
	if header.Size == 0 {
		writeError(w, http.StatusBadRequest, "recording is empty")
		return
	}
	contentType, _, _ := mime.ParseMediaType(header.Header.Get("Content-Type"))
	ext, ok := voiceAudioExtensions[strings.ToLower(contentType)]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported audio format")
		return
	}

	language := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if language != "" && !voiceLanguagePattern.MatchString(language) {
		writeError(w, http.StatusBadRequest, "invalid language")
		return
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if utf8.RuneCountInString(prompt) > maxVoicePromptRunes {
		prompt = string([]rune(prompt)[:maxVoicePromptRunes])
	}

	ctx, cancel := context.WithTimeout(r.Context(), voiceTranscribeTimeout)
	defer cancel()
	text, err := h.LLM.Transcribe(ctx, llm.TranscribeRequest{
		Model:       h.cfg.LLMTranscribeModel,
		Audio:       file,
		Filename:    "voice." + ext,
		ContentType: contentType,
		Language:    language,
		Prompt:      prompt,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.Warn("voice transcription failed", "error", err, "size", header.Size, "format", ext)
		writeError(w, http.StatusBadGateway, "transcription failed")
		return
	}
	writeJSON(w, http.StatusOK, voiceTranscriptionResponse{Text: text})
}

package handler

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/multica-ai/multica/server/internal/service"
)

var allowedTranscriptionContentTypes = map[string]bool{
	"audio/webm":      true,
	"audio/mp4":       true,
	"audio/mpeg":      true,
	"audio/wav":       true,
	"audio/wave":      true,
	"audio/x-wav":     true,
	"audio/ogg":       true,
	"video/webm":      true,
	"application/ogg": true,
}

// TranscribeAudio handles non-streaming audio transcription for issue voice capture.
func (h *Handler) TranscribeAudio(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	maxBytes := service.DefaultTranscriptionMaxBytes
	if h.TranscriptionService != nil && h.TranscriptionService.MaxBytes > 0 {
		maxBytes = h.TranscriptionService.MaxBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "audio file too large or invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "failed to read audio file")
		return
	}
	sniffed := http.DetectContentType(buf[:n])
	contentType := normalizeTranscriptionContentType(sniffed, header.Header.Get("Content-Type"), header.Filename)
	if !isAllowedTranscriptionContentType(contentType) {
		writeError(w, http.StatusBadRequest, "unsupported audio content type")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read audio file")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read audio file")
		return
	}
	if int64(len(data)) > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "audio file too large")
		return
	}

	result, err := h.TranscriptionService.Transcribe(r.Context(), service.TranscriptionInput{
		Filename:    header.Filename,
		ContentType: contentType,
		Data:        data,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTranscriptionProviderNotConfigured):
			writeError(w, http.StatusFailedDependency, "transcription provider is not configured")
		case errors.Is(err, service.ErrEmptyTranscript):
			writeError(w, http.StatusBadRequest, "no speech was detected")
		default:
			writeError(w, http.StatusBadGateway, "transcription failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// normalizeTranscriptionContentType prefers specific upload headers when sniffing is generic.
func normalizeTranscriptionContentType(sniffed, headerContentType, filename string) string {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(sniffed, ";")[0]))
	headerType := strings.ToLower(strings.TrimSpace(strings.Split(headerContentType, ";")[0]))
	if isAllowedTranscriptionContentType(headerType) && (contentType == "application/octet-stream" || contentType == "text/plain") {
		contentType = headerType
	}

	switch strings.ToLower(path.Ext(filename)) {
	case ".webm":
		if contentType == "" || contentType == "application/octet-stream" {
			return "audio/webm"
		}
	case ".mp4", ".m4a":
		if contentType == "" || contentType == "application/octet-stream" {
			return "audio/mp4"
		}
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".opus":
		if contentType == "" || contentType == "application/octet-stream" {
			return "audio/ogg"
		}
	}
	return contentType
}

// isAllowedTranscriptionContentType checks the Phase 1 audio MIME allowlist.
func isAllowedTranscriptionContentType(contentType string) bool {
	if allowedTranscriptionContentTypes[contentType] {
		return true
	}
	return strings.HasPrefix(contentType, "audio/")
}

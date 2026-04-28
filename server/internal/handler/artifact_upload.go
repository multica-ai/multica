package handler

import (
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"
)

// ArtifactUploadResponse is the result of POST /api/artifact-uploads — the
// client uses {url, size_bytes} as file_url + file_size_bytes when creating
// or updating a PDF/HTML artifact.
type ArtifactUploadResponse struct {
	URL          string `json:"url"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
}

// UploadArtifactFile — POST /api/artifact-uploads
// Multipart upload of a PDF or HTML file. Stores via the configured Storage
// adapter under workspaces/<id>/artifacts/<uuid>.<ext> and returns the
// public URL plus size.
func (h *Handler) UploadArtifactFile(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
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
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	contentType := http.DetectContentType(buf[:n])
	ext := strings.ToLower(path.Ext(header.Filename))
	if ct, ok := extContentTypes[ext]; ok {
		contentType = ct
	}
	if !strings.HasPrefix(contentType, "application/pdf") && !strings.HasPrefix(contentType, "text/html") {
		writeError(w, http.StatusUnsupportedMediaType, "only application/pdf or text/html accepted")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("uuid gen failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	key := "workspaces/" + workspaceID + "/artifacts/" + id.String() + ext

	url, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		slog.Error("artifact upload failed", "error", err, "user", userID)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}

	writeJSON(w, http.StatusOK, ArtifactUploadResponse{
		URL:         url,
		Filename:    header.Filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	})
}

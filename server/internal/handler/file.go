package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// extContentTypes overrides http.DetectContentType for extensions it gets wrong.
// Go's sniffer returns text/xml for SVG, text/plain for CSS/JS, etc.
var extContentTypes = map[string]string{
	".svg":  "image/svg+xml",
	".css":  "text/css",
	".js":   "application/javascript",
	".mjs":  "application/javascript",
	".json": "application/json",
	".wasm": "application/wasm",
}

const maxUploadSize = 100 << 20 // 100 MB

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type AttachmentResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	IssueID      *string `json:"issue_id"`
	CommentID    *string `json:"comment_id"`
	UploaderType string  `json:"uploader_type"`
	UploaderID   string  `json:"uploader_id"`
	Filename     string  `json:"filename"`
	URL          string  `json:"url"`
	DownloadURL  string  `json:"download_url"`
	ContentType  string  `json:"content_type"`
	SizeBytes    int64   `json:"size_bytes"`
	CreatedAt    string  `json:"created_at"`
}

func (h *Handler) attachmentToResponse(a db.Attachment) AttachmentResponse {
	resp := AttachmentResponse{
		ID:           uuidToString(a.ID),
		WorkspaceID:  uuidToString(a.WorkspaceID),
		UploaderType: a.UploaderType,
		UploaderID:   uuidToString(a.UploaderID),
		Filename:     a.Filename,
		URL:          a.Url,
		DownloadURL:  a.Url,
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		CreatedAt:    a.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if h.CFSigner != nil {
		resp.DownloadURL = h.CFSigner.SignedURL(a.Url, time.Now().Add(30*time.Minute))
	}
	if a.IssueID.Valid {
		s := uuidToString(a.IssueID)
		resp.IssueID = &s
	}
	if a.CommentID.Valid {
		s := uuidToString(a.CommentID)
		resp.CommentID = &s
	}
	return resp
}

// groupAttachments loads attachments for multiple comments and groups them by comment ID.
func (h *Handler) groupAttachments(r *http.Request, commentIDs []pgtype.UUID) map[string][]AttachmentResponse {
	if len(commentIDs) == 0 {
		return nil
	}
	attachments, err := h.Queries.ListAttachmentsByCommentIDs(r.Context(), commentIDs)
	if err != nil {
		slog.Error("failed to load attachments for comments", "error", err)
		return nil
	}
	grouped := make(map[string][]AttachmentResponse, len(commentIDs))
	for _, a := range attachments {
		cid := uuidToString(a.CommentID)
		grouped[cid] = append(grouped[cid], h.attachmentToResponse(a))
	}
	return grouped
}

// ---------------------------------------------------------------------------
// UploadFile — POST /api/upload-file
// ---------------------------------------------------------------------------

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("missing file field: %v", err))
		return
	}
	defer file.Close()

	// Sniff actual content type from file bytes instead of trusting the client header.
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	contentType := http.DetectContentType(buf[:n])
	// Override with extension-based type when the sniffer gets it wrong.
	if ct, ok := extContentTypes[strings.ToLower(path.Ext(header.Filename))]; ok {
		contentType = ct
	}
	// Seek back so the full file is uploaded.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	// Generate a UUIDv7 to use as both the attachment ID and S3 key.
	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	key := id.String() + path.Ext(header.Filename)

	link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		slog.Error("file upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}

	// If workspace context is available, create an attachment record.
	if workspaceID != "" {
		uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)

		params := db.CreateAttachmentParams{
			ID:           pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:  parseUUID(workspaceID),
			UploaderType: uploaderType,
			UploaderID:   parseUUID(uploaderID),
			Filename:     header.Filename,
			Url:          link,
			ContentType:  contentType,
			SizeBytes:    int64(len(data)),
		}

		// Optional issue_id / comment_id from form fields
		if issueID := r.FormValue("issue_id"); issueID != "" {
			params.IssueID = parseUUID(issueID)
		}
		if commentID := r.FormValue("comment_id"); commentID != "" {
			params.CommentID = parseUUID(commentID)
		}

		att, err := h.Queries.CreateAttachment(r.Context(), params)
		if err != nil {
			slog.Error("failed to create attachment record", "error", err)
			// S3 upload succeeded but DB record failed — still return the link
			// so the file is usable. Log the error for investigation.
		} else {
			writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
			return
		}
	}

	// Fallback response (no workspace context, e.g. avatar upload)
	writeJSON(w, http.StatusOK, map[string]string{
		"filename": header.Filename,
		"link":     link,
	})
}

// ---------------------------------------------------------------------------
// ListAttachments — GET /api/issues/{id}/attachments
// ---------------------------------------------------------------------------

func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Error("failed to list attachments", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list attachments")
		return
	}

	resp := make([]AttachmentResponse, len(attachments))
	for i, a := range attachments {
		resp[i] = h.attachmentToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// GetAttachmentByID — GET /api/attachments/{id}
// ---------------------------------------------------------------------------

func (h *Handler) GetAttachmentByID(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(attachmentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
}

// ---------------------------------------------------------------------------
// DeleteAttachment — DELETE /api/attachments/{id}
// ---------------------------------------------------------------------------

func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(attachmentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	// Only the uploader (or workspace admin) can delete
	uploaderID := uuidToString(att.UploaderID)
	isUploader := att.UploaderType == "member" && uploaderID == userID
	member, hasMember := ctxMember(r.Context())
	isAdmin := hasMember && (member.Role == "admin" || member.Role == "owner")

	if !isUploader && !isAdmin {
		writeError(w, http.StatusForbidden, "not authorized to delete this attachment")
		return
	}

	if err := h.Queries.DeleteAttachment(r.Context(), db.DeleteAttachmentParams{
		ID:          att.ID,
		WorkspaceID: att.WorkspaceID,
	}); err != nil {
		slog.Error("failed to delete attachment", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete attachment")
		return
	}

	h.deleteS3Object(r.Context(), att.Url)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Attachment linking
// ---------------------------------------------------------------------------

// linkAttachmentsByIssueIDs links the given attachment IDs to an issue.
// Only updates attachments that have no issue_id yet.
func (h *Handler) linkAttachmentsByIssueIDs(ctx context.Context, issueID, workspaceID pgtype.UUID, ids []string) {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = parseUUID(id)
	}
	if err := h.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		Column3:     uuids,
	}); err != nil {
		slog.Error("failed to link attachments to issue", "error", err)
	}
}

// linkAttachmentsByIDs links the given attachment IDs to a comment.
// Only updates attachments that belong to the same issue and have no comment_id yet.
func (h *Handler) linkAttachmentsByIDs(ctx context.Context, commentID, issueID pgtype.UUID, ids []string) {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = parseUUID(id)
	}
	if err := h.Queries.LinkAttachmentsToComment(ctx, db.LinkAttachmentsToCommentParams{
		CommentID: commentID,
		IssueID:   issueID,
		Column3:   uuids,
	}); err != nil {
		slog.Error("failed to link attachments to comment", "error", err)
	}
}

// deleteS3Object removes a single file from S3 by its CDN URL.
func (h *Handler) deleteS3Object(ctx context.Context, url string) {
	if h.Storage == nil || url == "" {
		return
	}
	h.Storage.Delete(ctx, h.Storage.KeyFromURL(url))
}

// deleteS3Objects removes multiple files from S3 by their CDN URLs.
func (h *Handler) deleteS3Objects(ctx context.Context, urls []string) {
	if h.Storage == nil || len(urls) == 0 {
		return
	}
	keys := make([]string, len(urls))
	for i, u := range urls {
		keys[i] = h.Storage.KeyFromURL(u)
	}
	h.Storage.DeleteKeys(ctx, keys)
}

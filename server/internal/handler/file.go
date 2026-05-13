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

// maxPreviewTextSize caps the body the preview proxy will load into memory
// for text-based types. Anything larger returns 413 and the UI falls back
// to "please download". Sized so a typical README/source-file fits but a
// 100 MB log dump can't blow up the renderer.
const maxPreviewTextSize = 2 << 20 // 2 MB

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type AttachmentResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	IssueID       *string `json:"issue_id"`
	CommentID     *string `json:"comment_id"`
	ChatSessionID *string `json:"chat_session_id"`
	ChatMessageID *string `json:"chat_message_id"`
	UploaderType  string  `json:"uploader_type"`
	UploaderID    string  `json:"uploader_id"`
	Filename      string  `json:"filename"`
	URL           string  `json:"url"`
	DownloadURL   string  `json:"download_url"`
	ContentType   string  `json:"content_type"`
	SizeBytes     int64   `json:"size_bytes"`
	CreatedAt     string  `json:"created_at"`
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
	if a.ChatSessionID.Valid {
		s := uuidToString(a.ChatSessionID)
		resp.ChatSessionID = &s
	}
	if a.ChatMessageID.Valid {
		s := uuidToString(a.ChatMessageID)
		resp.ChatMessageID = &s
	}
	return resp
}

// groupAttachments loads attachments for multiple comments and groups them by comment ID.
func (h *Handler) groupAttachments(r *http.Request, commentIDs []pgtype.UUID) map[string][]AttachmentResponse {
	if len(commentIDs) == 0 {
		return nil
	}
	workspaceID := h.resolveWorkspaceID(r)
	attachments, err := h.Queries.ListAttachmentsByCommentIDs(r.Context(), db.ListAttachmentsByCommentIDsParams{
		Column1:     commentIDs,
		WorkspaceID: parseUUID(workspaceID),
	})
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

// groupChatMessageAttachments loads attachments for multiple chat messages
// and groups them by chat_message_id. Mirrors groupAttachments — used so the
// chat message list can surface attachment metadata to the UI bubble (file
// cards, click-through download) without an N+1 query per message.
func (h *Handler) groupChatMessageAttachments(ctx context.Context, workspaceID string, messageIDs []pgtype.UUID) map[string][]AttachmentResponse {
	if len(messageIDs) == 0 {
		return nil
	}
	attachments, err := h.Queries.ListAttachmentsByChatMessageIDs(ctx, db.ListAttachmentsByChatMessageIDsParams{
		Column1:     messageIDs,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Error("failed to load attachments for chat messages", "error", err)
		return nil
	}
	grouped := make(map[string][]AttachmentResponse, len(messageIDs))
	for _, a := range attachments {
		mid := uuidToString(a.ChatMessageID)
		grouped[mid] = append(grouped[mid], h.attachmentToResponse(a))
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

	workspaceID := h.resolveWorkspaceID(r)

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
	filename := id.String() + path.Ext(header.Filename)
	var key string
	if workspaceID != "" {
		key = "workspaces/" + workspaceID + "/" + filename
	} else {
		key = "users/" + userID + "/" + filename
	}

	// If workspace context is available, validate membership before uploading.
	if workspaceID != "" {
		if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
			writeError(w, http.StatusForbidden, "not a member of this workspace")
			return
		}

		uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)

		params := db.CreateAttachmentParams{
			ID:           pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:  parseUUID(workspaceID),
			UploaderType: uploaderType,
			UploaderID:   parseUUID(uploaderID),
			Filename:     header.Filename,
			ContentType:  contentType,
			SizeBytes:    int64(len(data)),
		}

		if issueID := r.FormValue("issue_id"); issueID != "" {
			issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
			if !ok {
				return
			}
			issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          issueUUID,
				WorkspaceID: parseUUID(workspaceID),
			})
			if err != nil {
				writeError(w, http.StatusForbidden, "invalid issue_id")
				return
			}
			params.IssueID = issue.ID
		}
		if commentID := r.FormValue("comment_id"); commentID != "" {
			commentUUID, ok := parseUUIDOrBadRequest(w, commentID, "comment_id")
			if !ok {
				return
			}
			comment, err := h.Queries.GetComment(r.Context(), commentUUID)
			if err != nil || uuidToString(comment.WorkspaceID) != workspaceID {
				writeError(w, http.StatusForbidden, "invalid comment_id")
				return
			}
			params.CommentID = comment.ID
		}
		if chatSessionID := r.FormValue("chat_session_id"); chatSessionID != "" {
			// Re-use the existing private-agent gate so the user can still
			// reach this session — covers role downgrade and agent
			// visibility flips. The gate writes 4xx on failure.
			session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, chatSessionID)
			if !ok {
				return
			}
			params.ChatSessionID = session.ID
		}

		link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
		if err != nil {
			slog.Error("file upload failed", "error", err)
			writeError(w, http.StatusInternalServerError, "upload failed")
			return
		}
		params.Url = link

		att, err := h.Queries.CreateAttachment(r.Context(), params)
		if err != nil {
			slog.Error("failed to create attachment record", "error", err)
			// S3 upload succeeded but DB record failed — still return the link
			// so the file is usable. Log the error for investigation.
		} else {
			writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"id":       "",
			"url":      link,
			"filename": header.Filename,
		})
		return
	}

	// No workspace context (e.g. avatar upload) — upload directly.
	link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		slog.Error("file upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       id.String(),
		"url":      link,
		"filename": header.Filename,
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
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
}

// ---------------------------------------------------------------------------
// GetAttachmentContent — GET /api/attachments/{id}/content
//
// Streams the raw bytes of a text-previewable attachment back to the client.
// Exists to (a) bypass CloudFront CORS (not configured) and (b) bypass
// Content-Disposition: attachment which Chromium honors for iframe document
// loads. Media types (image/video/audio/pdf) intentionally do NOT go through
// this endpoint — clients render them directly from the CloudFront signed
// download_url, which already serves them with Content-Disposition: inline
// (see storage/util.go isInlineContentType).
//
// Hard cap: 2 MB. Larger files return 413. Anything outside the text
// whitelist returns 415.
// ---------------------------------------------------------------------------

func (h *Handler) GetAttachmentContent(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	if !isTextPreviewable(att.ContentType, att.Filename) {
		writeError(w, http.StatusUnsupportedMediaType, "preview not supported for this file type")
		return
	}

	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}
	key := h.Storage.KeyFromURL(att.Url)
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open attachment for preview", "id", attachmentID, "key", key, "error", err)
		writeError(w, http.StatusNotFound, "attachment object not found")
		return
	}
	defer reader.Close()

	// LimitReader to maxPreviewTextSize+1 so we can detect "exactly at the
	// limit" vs "exceeds the limit" by checking the returned length.
	body, err := io.ReadAll(io.LimitReader(reader, maxPreviewTextSize+1))
	if err != nil {
		slog.Error("failed to read attachment body for preview", "id", attachmentID, "error", err)
		writeError(w, http.StatusBadGateway, "failed to read attachment body")
		return
	}
	if len(body) > maxPreviewTextSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large for inline preview")
		return
	}

	// Always reply as text/plain so a hostile HTML payload can't be
	// re-interpreted as a document by the browser. The original MIME is
	// surfaced via X-Original-Content-Type for the client-side dispatcher.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Original-Content-Type", att.ContentType)
	// No-store: workspace membership / attachment ACL can change between
	// requests (member removed, attachment deleted). A cached body would
	// stay readable past the revocation window. The redundant request is
	// fine here — bodies are capped at 2 MB and the endpoint is only hit
	// when a user explicitly opens a preview.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write attachment preview body", "id", attachmentID, "error", err)
	}
}

// isTextPreviewable is the whitelist for the text preview proxy.
//
// IMPORTANT — KEEP IN SYNC with the client-side mirror in
// packages/views/editor/utils/preview.ts (TEXT_EXTENSIONS / TEXT_CONTENT_TYPES
// / TEXT_BASENAMES + extensionToLanguage). If a type is allowed here but not
// mapped client-side the user sees raw unhighlighted text; if mapped client-side
// but rejected here the user sees a 415 fallback.
//
// TODO(follow-up): extract this list to a JSON single-source-of-truth and
// generate the TS side, mirroring the reserved-slugs pattern (see
// server/internal/handler/reserved_slugs.json + scripts/generate-reserved-slugs.mjs).
// Drift severity here is low (worst case: Eye button visible but proxy 415s,
// modal shows the unsupported fallback — still functional, just confusing),
// so it ships as manual hand-sync for v1.
//
// We check both content_type and extension because http.DetectContentType
// regularly returns "text/plain" for Markdown / source code, so a pure
// content-type check would 415 those.
func isTextPreviewable(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Strip params (e.g. "text/plain; charset=utf-8")
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json",
		"application/javascript",
		"application/xml",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
		"application/x-sh",
		"application/x-httpd-php":
		return true
	}

	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".md", ".markdown",
		".txt", ".log",
		".csv", ".tsv",
		".html", ".htm",
		".json", ".xml",
		".yml", ".yaml", ".toml", ".ini", ".conf",
		".sh", ".bash", ".zsh",
		".py", ".rb", ".go", ".rs",
		".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".css", ".scss", ".sass", ".less",
		".sql",
		".java", ".kt", ".swift",
		".c", ".cc", ".cpp", ".h", ".hpp",
		".cs", ".php", ".lua", ".vim",
		".dockerfile", ".makefile", ".gitignore":
		return true
	}
	// Filenames without extension that match well-known build files.
	base := strings.ToLower(path.Base(filename))
	switch base {
	case "dockerfile", "makefile", ".env":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// DeleteAttachment — DELETE /api/attachments/{id}
// ---------------------------------------------------------------------------

func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
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
func (h *Handler) linkAttachmentsByIssueIDs(ctx context.Context, issueID, workspaceID pgtype.UUID, ids []pgtype.UUID) {
	if err := h.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		Column3:     ids,
	}); err != nil {
		slog.Error("failed to link attachments to issue", "error", err)
	}
}

// linkAttachmentsByIDs links the given attachment IDs to a comment.
// Only updates attachments that belong to the same issue and have no comment_id yet.
func (h *Handler) linkAttachmentsByIDs(ctx context.Context, commentID, issueID pgtype.UUID, ids []pgtype.UUID) {
	if err := h.Queries.LinkAttachmentsToComment(ctx, db.LinkAttachmentsToCommentParams{
		CommentID: commentID,
		IssueID:   issueID,
		Column3:   ids,
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

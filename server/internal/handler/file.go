package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/storage"
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

const defaultAttachmentDownloadURLTTL = 30 * time.Minute

type attachmentDownloadMode string

const (
	attachmentDownloadModeAuto       attachmentDownloadMode = "auto"
	attachmentDownloadModeCloudFront attachmentDownloadMode = "cloudfront"
	attachmentDownloadModePresign    attachmentDownloadMode = "presign"
	attachmentDownloadModeProxy      attachmentDownloadMode = "proxy"
)

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
	// MarkdownURL is the durable, absolute-when-possible URL the client
	// SHOULD persist into markdown bodies (issue descriptions, comments,
	// chat messages). It is computed per deployment policy by
	// buildMarkdownURL — preferring the storage URL when it is already a
	// public, durable absolute URL (public CDN / LocalStorage with
	// MULTICA_LOCAL_UPLOAD_BASE_URL), and otherwise prefixing
	// MULTICA_PUBLIC_URL onto the stable per-attachment endpoint that the
	// server self-resigns / proxies on every request.
	//
	// Why a separate field from URL / DownloadURL:
	//   - URL is the raw storage object URL — fine for avatar/logo
	//     surfaces but may be private (S3 + CloudFront-signed mode) or
	//     site-relative (LocalStorage with no base URL configured).
	//   - DownloadURL is the URL the renderer uses for THIS response — it
	//     can be a short-lived signed URL (CloudFront, S3 presign) and
	//     therefore must NOT be persisted. It expires.
	//   - MarkdownURL is contracted to be persistable: it never carries a
	//     TTL, and on every supported deployment shape it is loadable as
	//     a native browser resource fetch (no Authorization header required
	//     beyond the cookies/credentials the client already has on the
	//     resolved host).
	//
	// MUL-3192 — fixes the Desktop / mobile-webview regression where the
	// previous site-relative `/api/attachments/<id>/download` link only
	// resolved when the document origin proxied /api to the API host.
	MarkdownURL string `json:"markdown_url"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

func (h *Handler) attachmentToResponse(a db.Attachment) AttachmentResponse {
	id := uuidToString(a.ID)
	resp := AttachmentResponse{
		ID:           id,
		WorkspaceID:  uuidToString(a.WorkspaceID),
		UploaderType: a.UploaderType,
		UploaderID:   uuidToString(a.UploaderID),
		Filename:     a.Filename,
		URL:          a.Url,
		DownloadURL:  attachmentDownloadPath(id),
		MarkdownURL:  h.buildMarkdownURL(a, id),
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		CreatedAt:    a.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if h.CFSigner != nil {
		resp.DownloadURL = h.CFSigner.SignedURL(a.Url, time.Now().Add(h.attachmentDownloadURLTTL()))
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

func attachmentDownloadPath(id string) string {
	return "/api/attachments/" + id + "/download"
}

// buildMarkdownURL chooses the durable URL the client persists into
// markdown bodies. The contract is "absolute, no TTL, loadable as a native
// browser resource fetch on every supported client" (MUL-3192).
//
// Decision:
//
//  1. Persist `a.Url` only when the deployment has signaled the storage
//     backend serves URLs publicly without per-request auth:
//     - `Storage.CdnDomain()` is non-empty (operator configured a
//     public-facing base URL — `S3_CDN_DOMAIN` for the S3 backend or
//     `LOCAL_UPLOAD_BASE_URL` for LocalStorage), AND
//     - `h.CFSigner` is nil (no per-request CloudFront signing — when
//     signing is on, the same CDN domain serves PRIVATE content via
//     time-bounded signed URLs and the raw `a.Url` is unauth-deny),
//     AND
//     - `a.Url` is itself an absolute http(s) URL with no signature
//     query — defends against legacy rows backfilled while baseURL
//     was unset, and against a freshly-signed `download_url` ever
//     leaking into `a.Url` (the original MUL-3130 bug).
//
//  2. Every other shape — CloudFront-signed mode, S3 presign /proxy
//     against a private bucket without a CDN domain, raw S3 / R2 /
//     MinIO, LocalStorage with no `LOCAL_UPLOAD_BASE_URL` — uses the
//     stable per-attachment endpoint that the server self-signs /
//     proxies on every request, anchored on `MULTICA_PUBLIC_URL` so the
//     persisted URL keeps working for clients that don't share the
//     document origin (Desktop / mobile webview).
//
//  3. Last-resort fallback (no `MULTICA_PUBLIC_URL` configured): emit
//     the site-relative path. Web's Next.js rewrite handles this; non-
//     web clients on a deployment without `PublicURL` configured were
//     already broken before MUL-3192 and stay broken here, but we
//     don't make them worse.
func (h *Handler) buildMarkdownURL(a db.Attachment, id string) string {
	relPath := attachmentDownloadPath(id)
	publicURL := strings.TrimRight(h.cfg.PublicURL, "/")

	if h.storageURLIsPubliclyReadable(a.Url) {
		return a.Url
	}

	if publicURL != "" {
		return publicURL + relPath
	}
	return relPath
}

// storageURLIsPubliclyReadable returns true when the deployment has signaled
// that `a.Url` can be loaded directly by an unauthenticated native browser
// fetch — the only case where it is safe to persist `a.Url` into a markdown
// body that will outlive the current session.
func (h *Handler) storageURLIsPubliclyReadable(rawURL string) bool {
	if h.Storage == nil || h.CFSigner != nil {
		// CFSigner != nil is per-request signing; the CDN domain serves
		// private content via signed URLs and `a.Url` is the raw S3 URL.
		return false
	}
	if h.Storage.CdnDomain() == "" {
		// No public-facing base URL configured — the storage's URL is
		// the raw private object URL (S3 / R2 / MinIO) or a site-relative
		// LocalStorage path that doesn't carry an origin.
		return false
	}
	return isDurablePublicURL(rawURL)
}

// isDurablePublicURL is true when `rawURL` is an absolute http(s) URL that
// is safe to persist into long-lived markdown bodies — i.e. it carries no
// CloudFront / S3 signature query that would make it expire.
func isDurablePublicURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	q := u.Query()
	for _, k := range []string{
		"Signature",
		"X-Amz-Signature",
		"Key-Pair-Id",
		"Expires",
		"X-Amz-Expires",
	} {
		if q.Get(k) != "" {
			return false
		}
	}
	return true
}

func normalizeAttachmentDownloadMode(raw string) (attachmentDownloadMode, bool) {
	switch attachmentDownloadMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", attachmentDownloadModeAuto:
		return attachmentDownloadModeAuto, true
	case attachmentDownloadModeCloudFront:
		return attachmentDownloadModeCloudFront, true
	case attachmentDownloadModePresign:
		return attachmentDownloadModePresign, true
	case attachmentDownloadModeProxy:
		return attachmentDownloadModeProxy, true
	default:
		return attachmentDownloadModeAuto, false
	}
}

func (h *Handler) attachmentDownloadMode() attachmentDownloadMode {
	mode, _ := normalizeAttachmentDownloadMode(h.cfg.AttachmentDownloadMode)
	return mode
}

func (h *Handler) attachmentDownloadURLTTL() time.Duration {
	if h.cfg.AttachmentDownloadURLTTL > 0 {
		return h.cfg.AttachmentDownloadURLTTL
	}
	return defaultAttachmentDownloadURLTTL
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
	att, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
}

func (h *Handler) loadAttachmentForRequest(w http.ResponseWriter, r *http.Request) (db.Attachment, bool) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Attachment{}, false
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return db.Attachment{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Attachment{}, false
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}

	return att, true
}

// loadAttachmentForDownload is a workspace-self-resolving variant used by the
// /api/attachments/{id}/download endpoint. It looks the attachment up by ID
// alone, then enforces that the authenticated user is a member of the
// attachment's workspace.
//
// Why a separate code path: a native browser <img>/<video> resource load on
// /api/attachments/{id}/download cannot attach the X-Workspace-Slug /
// X-Workspace-ID headers that loadAttachmentForRequest relies on. Putting
// the workspace into the URL (?workspace_slug=...) would work mechanically
// but bakes a non-essential identifier into every persisted comment markdown
// link — unnecessary because the attachment row already records its
// workspace. This helper keeps the URL clean (`/api/attachments/{id}/download`)
// and treats the attachment id + cookie/Bearer auth as sufficient.
//
// Membership uses the same 404-on-deny shape as ServeLocalUpload so the
// route does not act as an IDOR oracle for attachment IDs that happen to
// belong to a different workspace. The membership cache fast path mirrors
// canReadWorkspaceUpload exactly.
func (h *Handler) loadAttachmentForDownload(w http.ResponseWriter, r *http.Request) (db.Attachment, bool) {
	attachmentID := chi.URLParam(r, "id")
	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return db.Attachment{}, false
	}
	att, err := h.Queries.GetAttachmentByIDOnly(r.Context(), attUUID)
	if err != nil {
		// 404 (not 403/401) so non-member and non-existent look identical
		// to outside callers. Same shape as ServeLocalUpload's
		// canReadWorkspaceUpload deny path.
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Attachment{}, false
	}

	workspaceID := uuidToString(att.WorkspaceID)
	if workspaceID == "" {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}
	if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
		return att, true
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}
	h.MembershipCache.Set(r.Context(), userID, workspaceID)
	return att, true
}

// ---------------------------------------------------------------------------
// DownloadAttachment — GET /api/attachments/{id}/download
// ---------------------------------------------------------------------------
//
// Workspace context is derived from the attachment row itself, not from
// X-Workspace-Slug / X-Workspace-ID headers. This is what lets a markdown
// `<img src="/api/attachments/{id}/download">` work as a native browser
// resource load: the browser cannot attach those headers to <img>/<video>
// fetches, so resolving via the attachment row is the only way to keep
// the URL stable across reloads (the previous design persisted a 30-min
// signed /uploads URL into the markdown body — that URL stopped working
// the moment the signature expired).
//
// Membership is enforced inside loadAttachmentForDownload with a 404 deny
// shape so the route doesn't IDOR-leak attachment IDs to non-members.

func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForDownload(w, r)
	if !ok {
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}

	key := h.Storage.KeyFromURL(att.Url)
	switch h.resolveAttachmentDownloadMode(att.Url) {
	case attachmentDownloadModeCloudFront:
		if h.CFSigner == nil {
			writeError(w, http.StatusInternalServerError, "cloudfront attachment downloads are not configured")
			return
		}
		h.setAttachmentPreviewSecurityHeaders(w)
		http.Redirect(
			w,
			r,
			h.CFSigner.SignedURLWithContentDisposition(
				att.Url,
				storage.AttachmentContentDisposition(att.Filename),
				time.Now().Add(h.attachmentDownloadURLTTL()),
			),
			http.StatusFound,
		)
	case attachmentDownloadModePresign:
		presigner, ok := h.Storage.(storage.DownloadPresigner)
		if !ok {
			writeError(w, http.StatusInternalServerError, "attachment storage does not support presigned downloads")
			return
		}
		signedURL, err := presigner.PresignGetWithContentDisposition(
			r.Context(),
			key,
			h.attachmentDownloadURLTTL(),
			storage.AttachmentContentDisposition(att.Filename),
		)
		if err != nil {
			slog.Error("failed to presign attachment download", "id", uuidToString(att.ID), "key", key, "error", err)
			writeError(w, http.StatusBadGateway, "failed to create download URL")
			return
		}
		h.setAttachmentPreviewSecurityHeaders(w)
		http.Redirect(w, r, signedURL, http.StatusFound)
	case attachmentDownloadModeProxy:
		h.proxyAttachmentDownload(w, r, att, key)
	default:
		writeError(w, http.StatusInternalServerError, "invalid attachment download mode")
	}
}

func (h *Handler) resolveAttachmentDownloadMode(rawURL string) attachmentDownloadMode {
	switch h.attachmentDownloadMode() {
	case attachmentDownloadModeCloudFront:
		return attachmentDownloadModeCloudFront
	case attachmentDownloadModePresign:
		return attachmentDownloadModePresign
	case attachmentDownloadModeProxy:
		return attachmentDownloadModeProxy
	}
	if h.CFSigner != nil {
		return attachmentDownloadModeCloudFront
	}
	if shouldProxyAttachmentURL(rawURL) {
		return attachmentDownloadModeProxy
	}
	if _, ok := h.Storage.(storage.DownloadPresigner); ok {
		return attachmentDownloadModePresign
	}
	return attachmentDownloadModeProxy
}

func shouldProxyAttachmentURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return true
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if !strings.Contains(host, ".") {
		return true
	}
	switch {
	case strings.HasSuffix(host, ".local"),
		strings.HasSuffix(host, ".localdomain"),
		strings.HasSuffix(host, ".internal"),
		strings.HasSuffix(host, ".lan"),
		strings.HasSuffix(host, ".home"),
		strings.HasSuffix(host, ".docker"):
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback() ||
			addr.IsPrivate() ||
			addr.IsLinkLocalUnicast() ||
			addr.IsLinkLocalMulticast() ||
			addr.IsUnspecified()
	}
	return false
}

func (h *Handler) proxyAttachmentDownload(w http.ResponseWriter, r *http.Request, att db.Attachment, key string) {
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open attachment for download", "id", uuidToString(att.ID), "key", key, "error", err)
		writeError(w, http.StatusNotFound, "attachment object not found")
		return
	}
	defer reader.Close()

	if att.ContentType != "" {
		w.Header().Set("Content-Type", att.ContentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if att.SizeBytes >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", att.SizeBytes))
	}
	w.Header().Set("Content-Disposition", storage.ContentDisposition(att.ContentType, att.Filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.setAttachmentPreviewSecurityHeaders(w)
	if _, err := io.Copy(w, reader); err != nil {
		slog.Error("failed to stream attachment download", "id", uuidToString(att.ID), "error", err)
	}
}

func (h *Handler) setAttachmentPreviewSecurityHeaders(w http.ResponseWriter) {
	// Attachment preview responses may be loaded by the web app in same-origin
	// deployments or split app/api self-hosted deployments. Allow only the API
	// origin itself plus configured frontend/CORS origins.
	w.Header().Set("Content-Security-Policy", attachmentPreviewCSPHeader(h.cfg.AttachmentFrameAncestors))
}

func attachmentPreviewCSPHeader(frameAncestors []string) string {
	ancestors := []string{"'self'"}
	seen := map[string]struct{}{"'self'": {}}
	for _, raw := range frameAncestors {
		source, ok := normalizeFrameAncestorSource(raw)
		if !ok {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		ancestors = append(ancestors, source)
	}
	return "default-src 'none'; " +
		"img-src 'self' data:; " +
		"media-src 'self'; " +
		"frame-ancestors " + strings.Join(ancestors, " ") + "; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'none'"
}

func normalizeFrameAncestorSource(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

// ---------------------------------------------------------------------------
// GetAttachmentContent — GET /api/attachments/{id}/content
//
// Streams the raw bytes of a text-previewable attachment back to the client.
// Exists to (a) bypass CloudFront CORS (not configured) and (b) bypass
// Content-Disposition: attachment which Chromium honors for iframe document
// loads. Media types (image/video/audio/pdf) intentionally use download_url
// instead. Metadata download_url keeps CloudFront/S3's media preview behavior;
// the explicit /download route signs redirects as attachment downloads and
// proxy mode streams with the same media-type policy as storage uploads.
//
// Hard cap: 2 MB. Larger files return 413. Anything outside the text
// whitelist returns 415.
// ---------------------------------------------------------------------------

func (h *Handler) GetAttachmentContent(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}
	attachmentID := uuidToString(att.ID)

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
	h.setAttachmentPreviewSecurityHeaders(w)
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

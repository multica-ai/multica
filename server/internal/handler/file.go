package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
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

const maxUploadSize = 500 << 20 // 500 MB
const directUploadTTL = 30 * time.Minute
const multipartUploadTTL = 24 * time.Hour
const minMultipartPartSize = 5 << 20 // 5 MB, except for the final part
const maxMultipartParts = 10000

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
// 500 MB log dump can't blow up the renderer.
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

type attachmentUploadInitiateRequest struct {
	Filename      string  `json:"filename"`
	ContentType   string  `json:"content_type"`
	SizeBytes     int64   `json:"size_bytes"`
	IssueID       *string `json:"issue_id"`
	CommentID     *string `json:"comment_id"`
	ChatSessionID *string `json:"chat_session_id"`
}

type attachmentUploadInitiateResponse struct {
	AttachmentID string            `json:"attachment_id"`
	ObjectKey    string            `json:"object_key"`
	UploadURL    string            `json:"upload_url"`
	Headers      map[string]string `json:"headers"`
	UploadToken  string            `json:"upload_token"`
	ExpiresAt    string            `json:"expires_at"`
}

type attachmentUploadCompleteRequest struct {
	UploadToken string `json:"upload_token"`
}

type multipartUploadInitiateRequest struct {
	Filename      string  `json:"filename"`
	ContentType   string  `json:"content_type"`
	SizeBytes     int64   `json:"size_bytes"`
	PartSizeBytes int64   `json:"part_size_bytes"`
	IssueID       *string `json:"issue_id"`
	CommentID     *string `json:"comment_id"`
	ChatSessionID *string `json:"chat_session_id"`
}

type multipartUploadInitiateResponse struct {
	SessionID     string            `json:"session_id"`
	AttachmentID  string            `json:"attachment_id"`
	ObjectKey     string            `json:"object_key"`
	UploadID      string            `json:"upload_id"`
	Headers       map[string]string `json:"headers"`
	PartSizeBytes int64             `json:"part_size_bytes"`
	PartCount     int32             `json:"part_count"`
	ExpiresAt     string            `json:"expires_at"`
}

type multipartUploadSignPartsRequest struct {
	SessionID   string  `json:"session_id"`
	PartNumbers []int32 `json:"part_numbers"`
}

type multipartUploadPartURL struct {
	PartNumber int32             `json:"part_number"`
	UploadURL  string            `json:"upload_url"`
	Headers    map[string]string `json:"headers"`
}

type multipartUploadSignPartsResponse struct {
	Parts     []multipartUploadPartURL `json:"parts"`
	ExpiresAt string                   `json:"expires_at"`
}

type multipartCompletedPartRequest struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"size_bytes"`
}

type multipartUploadCompleteRequest struct {
	SessionID string                          `json:"session_id"`
	Parts     []multipartCompletedPartRequest `json:"parts"`
}

type multipartUploadAbortRequest struct {
	SessionID string `json:"session_id"`
}

type directUploadClaims struct {
	AttachmentID  string  `json:"attachment_id"`
	ObjectKey     string  `json:"object_key"`
	WorkspaceID   string  `json:"workspace_id"`
	UploaderType  string  `json:"uploader_type"`
	UploaderID    string  `json:"uploader_id"`
	Filename      string  `json:"filename"`
	ContentType   string  `json:"content_type"`
	SizeBytes     int64   `json:"size_bytes"`
	IssueID       *string `json:"issue_id,omitempty"`
	CommentID     *string `json:"comment_id,omitempty"`
	ChatSessionID *string `json:"chat_session_id,omitempty"`
	UserID        string  `json:"user_id"`
	ExpiresAt     int64   `json:"expires_at"`
}

func (h *Handler) attachmentToResponse(a db.Attachment) AttachmentResponse {
	id := uuidToString(a.ID)
	remappedURL := h.Storage.RemapURL(a.Url)
	resp := AttachmentResponse{
		ID:           id,
		WorkspaceID:  uuidToString(a.WorkspaceID),
		UploaderType: a.UploaderType,
		UploaderID:   uuidToString(a.UploaderID),
		Filename:     a.Filename,
		URL:          remappedURL,
		DownloadURL:  attachmentDownloadPath(id),
		MarkdownURL:  h.buildMarkdownURL(a, id),
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		CreatedAt:    a.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if h.CFSigner != nil {
		resp.DownloadURL = h.CFSigner.SignedURL(remappedURL, time.Now().Add(h.attachmentDownloadURLTTL()))
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
//       - `Storage.CdnDomain()` is non-empty (operator configured a
//         public-facing base URL — `S3_CDN_DOMAIN` for the S3 backend or
//         `LOCAL_UPLOAD_BASE_URL` for LocalStorage), AND
//       - `h.CFSigner` is nil (no per-request CloudFront signing — when
//         signing is on, the same CDN domain serves PRIVATE content via
//         time-bounded signed URLs and the raw `a.Url` is unauth-deny),
//         AND
//       - `a.Url` is itself an absolute http(s) URL with no signature
//         query — defends against legacy rows backfilled while baseURL
//         was unset, and against a freshly-signed `download_url` ever
//         leaking into `a.Url` (the original MUL-3130 bug).
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

func uploadContentType(filename, declared string) string {
	contentType := strings.TrimSpace(declared)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if ct, ok := extContentTypes[strings.ToLower(path.Ext(filename))]; ok {
		contentType = ct
	}
	return contentType
}

func uploadObjectKey(workspaceID, userID, attachmentID, filename string) string {
	return uploadObjectKeyWithPrefix(s3KeyPrefixFromEnv(), workspaceID, userID, attachmentID, filename)
}

func s3KeyPrefixFromEnv() string {
	return normalizeUploadKeyPrefix(os.Getenv("S3_KEY_PREFIX"))
}

func storageKeyPrefix(store storage.Storage) string {
	if prefixed, ok := store.(storage.KeyPrefixStorage); ok {
		return normalizeUploadKeyPrefix(prefixed.KeyPrefix())
	}
	return ""
}

func uploadObjectKeyWithPrefix(prefix, workspaceID, userID, attachmentID, filename string) string {
	storedName := attachmentID + path.Ext(filename)
	base := ""
	if workspaceID != "" {
		base = "workspaces/" + workspaceID + "/" + storedName
	} else {
		base = "users/" + userID + "/" + storedName
	}
	prefix = normalizeUploadKeyPrefix(prefix)
	if prefix == "" {
		return base
	}
	return prefix + "/" + base
}

func workspaceObjectKeyPrefix(prefix, workspaceID string) string {
	base := "workspaces/" + workspaceID + "/"
	prefix = normalizeUploadKeyPrefix(prefix)
	if prefix == "" {
		return base
	}
	return prefix + "/" + base
}

func normalizeUploadKeyPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.Trim(prefix, "/")
	if prefix == "." {
		return ""
	}
	return prefix
}

func (h *Handler) buildAttachmentParams(w http.ResponseWriter, r *http.Request, userID, workspaceID string, id uuid.UUID, filename, contentType string, sizeBytes int64, issueID, commentID, chatSessionID string) (db.CreateAttachmentParams, bool) {
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.CreateAttachmentParams{}, false
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return db.CreateAttachmentParams{}, false
	}

	uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)
	params := db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  parseUUID(workspaceID),
		UploaderType: uploaderType,
		UploaderID:   parseUUID(uploaderID),
		Filename:     filename,
		ContentType:  contentType,
		SizeBytes:    sizeBytes,
	}

	if issueID != "" {
		issueUUID, ok := h.resolveAttachmentIssueID(w, r, issueID, workspaceID)
		if !ok {
			return db.CreateAttachmentParams{}, false
		}
		params.IssueID = issueUUID
	}
	if commentID != "" {
		commentUUID, ok := parseUUIDOrBadRequest(w, commentID, "comment_id")
		if !ok {
			return db.CreateAttachmentParams{}, false
		}
		comment, err := h.Queries.GetComment(r.Context(), commentUUID)
		if err != nil || uuidToString(comment.WorkspaceID) != workspaceID {
			writeError(w, http.StatusForbidden, "invalid comment_id")
			return db.CreateAttachmentParams{}, false
		}
		params.CommentID = comment.ID
	}
	if chatSessionID != "" {
		session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, chatSessionID)
		if !ok {
			return db.CreateAttachmentParams{}, false
		}
		params.ChatSessionID = session.ID
	}
	return params, true
}

func (h *Handler) resolveAttachmentIssueID(w http.ResponseWriter, r *http.Request, issueID, workspaceID string) (pgtype.UUID, bool) {
	if issue, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		return issue.ID, true
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid issue_id")
		return pgtype.UUID{}, false
	}
	return issue.ID, true
}

func (h *Handler) validateAttachmentLinks(w http.ResponseWriter, r *http.Request, userID, workspaceID string, issueID, commentID, chatSessionID string) (db.CreateAttachmentParams, bool) {
	params, ok := h.buildAttachmentParams(w, r, userID, workspaceID, uuid.Nil, "", "", 0, issueID, commentID, chatSessionID)
	return params, ok
}

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
	contentType = uploadContentType(header.Filename, contentType)
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
	key := uploadObjectKeyWithPrefix(storageKeyPrefix(h.Storage), workspaceID, userID, id.String(), header.Filename)

	// If workspace context is available, validate membership before uploading.
	if workspaceID != "" {
		params, ok := h.buildAttachmentParams(w, r, userID, workspaceID, id, header.Filename, contentType, int64(len(data)), r.FormValue("issue_id"), r.FormValue("comment_id"), r.FormValue("chat_session_id"))
		if !ok {
			return
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
			h.deleteS3Object(r.Context(), link)
			writeError(w, http.StatusInternalServerError, "failed to create attachment record")
			return
		}

		writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
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

func signDirectUploadClaims(claims directUploadClaims) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(encodedBody))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedBody + "." + sig, nil
}

func verifyDirectUploadToken(token string) (directUploadClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return directUploadClaims{}, fmt.Errorf("invalid upload token")
	}
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, expected) {
		return directUploadClaims{}, fmt.Errorf("invalid upload token signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return directUploadClaims{}, fmt.Errorf("invalid upload token body")
	}
	var claims directUploadClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return directUploadClaims{}, fmt.Errorf("invalid upload token claims")
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return directUploadClaims{}, fmt.Errorf("upload token expired")
	}
	return claims, nil
}

// InitiateAttachmentUpload returns a presigned object-store PUT URL for direct upload.
func (h *Handler) InitiateAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	direct, ok := h.Storage.(storage.DirectUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "direct upload not supported")
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

	var req attachmentUploadInitiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Filename = strings.TrimSpace(req.Filename)
	if req.Filename == "" {
		writeError(w, http.StatusBadRequest, "filename is required")
		return
	}
	if req.SizeBytes < 0 || req.SizeBytes > maxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	contentType := uploadContentType(req.Filename, req.ContentType)
	issueID := ""
	commentID := ""
	chatSessionID := ""
	if req.IssueID != nil {
		issueID = *req.IssueID
	}
	if req.CommentID != nil {
		commentID = *req.CommentID
	}
	if req.ChatSessionID != nil {
		chatSessionID = *req.ChatSessionID
	}
	params, ok := h.buildAttachmentParams(w, r, userID, workspaceID, id, req.Filename, contentType, req.SizeBytes, issueID, commentID, chatSessionID)
	if !ok {
		return
	}

	objectKey := uploadObjectKeyWithPrefix(storageKeyPrefix(h.Storage), workspaceID, userID, id.String(), req.Filename)
	uploadURL, headers, err := direct.CreatePresignedPutURL(r.Context(), objectKey, contentType, req.Filename, directUploadTTL)
	if err != nil {
		slog.Error("failed to create direct upload URL", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}
	expiresAt := time.Now().Add(directUploadTTL).UTC()
	var issueIDPtr, commentIDPtr, chatSessionIDPtr *string
	if params.IssueID.Valid {
		s := uuidToString(params.IssueID)
		issueIDPtr = &s
	}
	if params.CommentID.Valid {
		s := uuidToString(params.CommentID)
		commentIDPtr = &s
	}
	if params.ChatSessionID.Valid {
		s := uuidToString(params.ChatSessionID)
		chatSessionIDPtr = &s
	}
	token, err := signDirectUploadClaims(directUploadClaims{
		AttachmentID:  id.String(),
		ObjectKey:     objectKey,
		WorkspaceID:   workspaceID,
		UploaderType:  params.UploaderType,
		UploaderID:    uuidToString(params.UploaderID),
		Filename:      req.Filename,
		ContentType:   contentType,
		SizeBytes:     req.SizeBytes,
		IssueID:       issueIDPtr,
		CommentID:     commentIDPtr,
		ChatSessionID: chatSessionIDPtr,
		UserID:        userID,
		ExpiresAt:     expiresAt.Unix(),
	})
	if err != nil {
		slog.Error("failed to sign direct upload token", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}

	writeJSON(w, http.StatusOK, attachmentUploadInitiateResponse{
		AttachmentID: id.String(),
		ObjectKey:    objectKey,
		UploadURL:    uploadURL,
		Headers:      headers,
		UploadToken:  token,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	})
}

// CompleteAttachmentUpload verifies the uploaded object and creates the attachment record.
func (h *Handler) CompleteAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	direct, ok := h.Storage.(storage.DirectUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "direct upload not supported")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req attachmentUploadCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims, err := verifyDirectUploadToken(req.UploadToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if claims.UserID != userID {
		writeError(w, http.StatusForbidden, "upload token belongs to a different user")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != claims.WorkspaceID {
		writeError(w, http.StatusForbidden, "upload token belongs to a different workspace")
		return
	}
	expectedPrefix := workspaceObjectKeyPrefix(storageKeyPrefix(h.Storage), claims.WorkspaceID)
	if !strings.HasPrefix(claims.ObjectKey, expectedPrefix) {
		writeError(w, http.StatusForbidden, "invalid upload object key")
		return
	}

	info, err := direct.HeadObject(r.Context(), claims.ObjectKey)
	if err != nil {
		slog.Error("direct upload object verification failed", "key", claims.ObjectKey, "error", err)
		writeError(w, http.StatusBadRequest, "uploaded object not found")
		return
	}
	if info.SizeBytes != claims.SizeBytes {
		writeError(w, http.StatusBadRequest, "uploaded object size mismatch")
		return
	}
	contentType := claims.ContentType
	if strings.TrimSpace(info.ContentType) != "" {
		contentType = uploadContentType(claims.Filename, info.ContentType)
	}
	attachmentID, ok := parseUUIDOrBadRequest(w, claims.AttachmentID, "attachment_id")
	if !ok {
		return
	}
	issueID := ""
	commentID := ""
	chatSessionID := ""
	if claims.IssueID != nil {
		issueID = *claims.IssueID
	}
	if claims.CommentID != nil {
		commentID = *claims.CommentID
	}
	if claims.ChatSessionID != nil {
		chatSessionID = *claims.ChatSessionID
	}
	validatedParams, ok := h.validateAttachmentLinks(w, r, userID, claims.WorkspaceID, issueID, commentID, chatSessionID)
	if !ok {
		return
	}
	params := db.CreateAttachmentParams{
		ID:           attachmentID,
		WorkspaceID:  parseUUID(claims.WorkspaceID),
		UploaderType: claims.UploaderType,
		UploaderID:   parseUUID(claims.UploaderID),
		Filename:     claims.Filename,
		ContentType:  contentType,
		SizeBytes:    claims.SizeBytes,
	}
	if claims.IssueID != nil {
		params.IssueID = validatedParams.IssueID
	}
	if claims.CommentID != nil {
		params.CommentID = validatedParams.CommentID
	}
	if claims.ChatSessionID != nil {
		params.ChatSessionID = validatedParams.ChatSessionID
	}
	params.Url = direct.PublicURL(claims.ObjectKey)

	att, err := h.Queries.CreateAttachment(r.Context(), params)
	if err != nil {
		slog.Error("failed to create direct upload attachment record", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create attachment record")
		return
	}
	writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
}

func multipartPartCount(sizeBytes, partSizeBytes int64) int32 {
	if sizeBytes <= 0 || partSizeBytes <= 0 {
		return 0
	}
	return int32((sizeBytes + partSizeBytes - 1) / partSizeBytes)
}

func normalizeMultipartPartSize(sizeBytes, requested int64) int64 {
	partSize := requested
	if partSize < minMultipartPartSize {
		partSize = minMultipartPartSize
	}
	for multipartPartCount(sizeBytes, partSize) > maxMultipartParts {
		partSize += minMultipartPartSize
	}
	return partSize
}

func requestStringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func multipartSessionIDFromRequest(w http.ResponseWriter, raw string) (pgtype.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return pgtype.UUID{}, false
	}
	return parseUUIDOrBadRequest(w, raw, "session_id")
}

func (h *Handler) loadMultipartUploadSession(w http.ResponseWriter, r *http.Request, sessionID pgtype.UUID, workspaceID string) (db.AttachmentUploadSession, bool) {
	session, err := h.Queries.GetAttachmentUploadSessionForUpdate(r.Context(), db.GetAttachmentUploadSessionForUpdateParams{
		ID:          sessionID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "upload session not found")
		return db.AttachmentUploadSession{}, false
	}
	if session.Status != "pending" {
		writeError(w, http.StatusBadRequest, "upload session is not pending")
		return db.AttachmentUploadSession{}, false
	}
	if time.Now().After(session.ExpiresAt.Time) {
		if err := h.Queries.MarkAttachmentUploadSessionExpired(r.Context(), db.MarkAttachmentUploadSessionExpiredParams{
			ID:          session.ID,
			WorkspaceID: session.WorkspaceID,
		}); err != nil {
			slog.Error("failed to expire upload session", "error", err)
		}
		writeError(w, http.StatusBadRequest, "upload session expired")
		return db.AttachmentUploadSession{}, false
	}
	return session, true
}

func (h *Handler) validateMultipartSessionActor(w http.ResponseWriter, r *http.Request, userID string, session db.AttachmentUploadSession) bool {
	if uuidToString(session.WorkspaceID) != h.resolveWorkspaceID(r) {
		writeError(w, http.StatusForbidden, "upload session belongs to a different workspace")
		return false
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(session.WorkspaceID)); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return false
	}
	currentUploaderType, currentUploaderID := h.resolveActor(r, userID, uuidToString(session.WorkspaceID))
	if session.UploaderType != currentUploaderType || uuidToString(session.UploaderID) != currentUploaderID {
		writeError(w, http.StatusForbidden, "upload session belongs to a different uploader")
		return false
	}
	return true
}

func validateMultipartParts(sizeBytes, partSizeBytes int64, parts []multipartCompletedPartRequest) ([]storage.MultipartUploadPart, error) {
	partCount := multipartPartCount(sizeBytes, partSizeBytes)
	if partCount == 0 {
		return nil, fmt.Errorf("invalid multipart upload size")
	}
	if len(parts) != int(partCount) {
		return nil, fmt.Errorf("multipart part count mismatch")
	}
	seen := make(map[int32]bool, len(parts))
	total := int64(0)
	completed := make([]storage.MultipartUploadPart, 0, len(parts))
	for _, part := range parts {
		if part.PartNumber < 1 || part.PartNumber > partCount {
			return nil, fmt.Errorf("invalid multipart part number")
		}
		if seen[part.PartNumber] {
			return nil, fmt.Errorf("duplicate multipart part number")
		}
		seen[part.PartNumber] = true
		etag := strings.TrimSpace(part.ETag)
		if etag == "" {
			return nil, fmt.Errorf("multipart part etag is required")
		}
		expectedSize := partSizeBytes
		if part.PartNumber == partCount {
			expectedSize = sizeBytes - int64(partCount-1)*partSizeBytes
		}
		if part.SizeBytes != expectedSize {
			return nil, fmt.Errorf("multipart part size mismatch")
		}
		total += part.SizeBytes
		completed = append(completed, storage.MultipartUploadPart{
			PartNumber: part.PartNumber,
			ETag:       etag,
		})
	}
	if total != sizeBytes {
		return nil, fmt.Errorf("multipart total size mismatch")
	}
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].PartNumber < completed[j].PartNumber
	})
	return completed, nil
}

func (h *Handler) InitiateMultipartAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	multipartStore, ok := h.Storage.(storage.MultipartUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "multipart upload not supported")
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

	var req multipartUploadInitiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Filename = strings.TrimSpace(req.Filename)
	if req.Filename == "" {
		writeError(w, http.StatusBadRequest, "filename is required")
		return
	}
	if req.SizeBytes < 0 || req.SizeBytes > maxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	attachmentID, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate attachment uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate upload session uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	contentType := uploadContentType(req.Filename, req.ContentType)
	params, ok := h.buildAttachmentParams(w, r, userID, workspaceID, attachmentID, req.Filename, contentType, req.SizeBytes, requestStringValue(req.IssueID), requestStringValue(req.CommentID), requestStringValue(req.ChatSessionID))
	if !ok {
		return
	}
	partSize := normalizeMultipartPartSize(req.SizeBytes, req.PartSizeBytes)
	partCount := multipartPartCount(req.SizeBytes, partSize)
	if partCount < 1 || partCount > maxMultipartParts {
		writeError(w, http.StatusBadRequest, "invalid multipart part count")
		return
	}
	objectKey := uploadObjectKeyWithPrefix(storageKeyPrefix(h.Storage), workspaceID, userID, attachmentID.String(), req.Filename)
	uploadID, headers, err := multipartStore.CreateMultipartUpload(r.Context(), objectKey, contentType, req.Filename, multipartUploadTTL)
	if err != nil {
		slog.Error("failed to create multipart upload", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate multipart upload")
		return
	}
	expiresAt := time.Now().Add(multipartUploadTTL).UTC()
	session, err := h.Queries.CreateAttachmentUploadSession(r.Context(), db.CreateAttachmentUploadSessionParams{
		ID:            pgtype.UUID{Bytes: sessionID, Valid: true},
		WorkspaceID:   parseUUID(workspaceID),
		AttachmentID:  pgtype.UUID{Bytes: attachmentID, Valid: true},
		ObjectKey:     objectKey,
		UploadID:      uploadID,
		Filename:      req.Filename,
		ContentType:   contentType,
		SizeBytes:     req.SizeBytes,
		PartSizeBytes: partSize,
		UploaderType:  params.UploaderType,
		UploaderID:    params.UploaderID,
		ExpiresAt:     pgtype.Timestamptz{Time: expiresAt, Valid: true},
		IssueID:       params.IssueID,
		CommentID:     params.CommentID,
		ChatSessionID: params.ChatSessionID,
	})
	if err != nil {
		_ = multipartStore.AbortMultipartUpload(r.Context(), objectKey, uploadID)
		slog.Error("failed to create upload session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate multipart upload")
		return
	}

	writeJSON(w, http.StatusOK, multipartUploadInitiateResponse{
		SessionID:     uuidToString(session.ID),
		AttachmentID:  uuidToString(session.AttachmentID),
		ObjectKey:     session.ObjectKey,
		UploadID:      session.UploadID,
		Headers:       headers,
		PartSizeBytes: partSize,
		PartCount:     partCount,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
	})
}

func (h *Handler) SignMultipartAttachmentUploadParts(w http.ResponseWriter, r *http.Request) {
	multipartStore, ok := h.Storage.(storage.MultipartUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "multipart upload not supported")
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
	var req multipartUploadSignPartsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sessionID, ok := multipartSessionIDFromRequest(w, req.SessionID)
	if !ok {
		return
	}
	session, ok := h.loadMultipartUploadSession(w, r, sessionID, workspaceID)
	if !ok {
		return
	}
	if !h.validateMultipartSessionActor(w, r, userID, session) {
		return
	}
	partCount := multipartPartCount(session.SizeBytes, session.PartSizeBytes)
	if len(req.PartNumbers) == 0 || len(req.PartNumbers) > 1000 {
		writeError(w, http.StatusBadRequest, "part_numbers is required")
		return
	}
	seen := make(map[int32]bool, len(req.PartNumbers))
	expiresAt := time.Now().Add(directUploadTTL).UTC()
	resp := multipartUploadSignPartsResponse{
		Parts:     make([]multipartUploadPartURL, 0, len(req.PartNumbers)),
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}
	for _, partNumber := range req.PartNumbers {
		if partNumber < 1 || partNumber > partCount || seen[partNumber] {
			writeError(w, http.StatusBadRequest, "invalid part number")
			return
		}
		seen[partNumber] = true
		uploadURL, headers, err := multipartStore.CreatePresignedUploadPartURL(r.Context(), session.ObjectKey, session.UploadID, partNumber, directUploadTTL)
		if err != nil {
			slog.Error("failed to presign multipart upload part", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to sign multipart upload part")
			return
		}
		resp.Parts = append(resp.Parts, multipartUploadPartURL{
			PartNumber: partNumber,
			UploadURL:  uploadURL,
			Headers:    headers,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CompleteMultipartAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	multipartStore, ok := h.Storage.(storage.MultipartUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "multipart upload not supported")
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
	if h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "database transactions not configured")
		return
	}
	var req multipartUploadCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sessionID, ok := multipartSessionIDFromRequest(w, req.SessionID)
	if !ok {
		return
	}
	session, ok := h.loadMultipartUploadSession(w, r, sessionID, workspaceID)
	if !ok {
		return
	}
	if !h.validateMultipartSessionActor(w, r, userID, session) {
		return
	}
	completedParts, err := validateMultipartParts(session.SizeBytes, session.PartSizeBytes, req.Parts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	expectedPrefix := workspaceObjectKeyPrefix(storageKeyPrefix(h.Storage), workspaceID)
	if !strings.HasPrefix(session.ObjectKey, expectedPrefix) {
		writeError(w, http.StatusForbidden, "invalid upload object key")
		return
	}
	issueID := ""
	commentID := ""
	chatSessionID := ""
	if session.IssueID.Valid {
		issueID = uuidToString(session.IssueID)
	}
	if session.CommentID.Valid {
		commentID = uuidToString(session.CommentID)
	}
	if session.ChatSessionID.Valid {
		chatSessionID = uuidToString(session.ChatSessionID)
	}
	validatedParams, ok := h.validateAttachmentLinks(w, r, userID, workspaceID, issueID, commentID, chatSessionID)
	if !ok {
		return
	}
	if err := multipartStore.CompleteMultipartUpload(r.Context(), session.ObjectKey, session.UploadID, completedParts); err != nil {
		slog.Error("failed to complete multipart upload", "error", err)
		writeError(w, http.StatusBadRequest, "failed to complete multipart upload")
		return
	}
	info, err := multipartStore.HeadObject(r.Context(), session.ObjectKey)
	if err != nil {
		slog.Error("multipart upload object verification failed", "key", session.ObjectKey, "error", err)
		writeError(w, http.StatusBadRequest, "uploaded object not found")
		return
	}
	if info.SizeBytes != session.SizeBytes {
		writeError(w, http.StatusBadRequest, "uploaded object size mismatch")
		return
	}

	params := db.CreateAttachmentParams{
		ID:           session.AttachmentID,
		WorkspaceID:  session.WorkspaceID,
		UploaderType: session.UploaderType,
		UploaderID:   session.UploaderID,
		Filename:     session.Filename,
		ContentType:  session.ContentType,
		SizeBytes:    session.SizeBytes,
		Url:          multipartStore.PublicURL(session.ObjectKey),
	}
	if strings.TrimSpace(info.ContentType) != "" {
		params.ContentType = uploadContentType(session.Filename, info.ContentType)
	}
	if session.IssueID.Valid {
		params.IssueID = validatedParams.IssueID
	}
	if session.CommentID.Valid {
		params.CommentID = validatedParams.CommentID
	}
	if session.ChatSessionID.Valid {
		params.ChatSessionID = validatedParams.ChatSessionID
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("failed to begin multipart upload transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete multipart upload")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err = qtx.GetAttachmentUploadSessionForUpdate(r.Context(), db.GetAttachmentUploadSessionForUpdateParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	})
	if err != nil || session.Status != "pending" {
		writeError(w, http.StatusBadRequest, "upload session is not pending")
		return
	}
	att, err := qtx.CreateAttachment(r.Context(), params)
	if err != nil {
		slog.Error("failed to create multipart upload attachment record", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create attachment record")
		return
	}
	if err := qtx.MarkAttachmentUploadSessionCompleted(r.Context(), db.MarkAttachmentUploadSessionCompletedParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		slog.Error("failed to mark upload session completed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete upload session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("failed to commit multipart upload transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete multipart upload")
		return
	}
	writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
}

func (h *Handler) AbortMultipartAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	multipartStore, ok := h.Storage.(storage.MultipartUploadStorage)
	if h.Storage == nil || !ok {
		writeError(w, http.StatusNotImplemented, "multipart upload not supported")
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
	var req multipartUploadAbortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sessionID, ok := multipartSessionIDFromRequest(w, req.SessionID)
	if !ok {
		return
	}
	session, ok := h.loadMultipartUploadSession(w, r, sessionID, workspaceID)
	if !ok {
		return
	}
	if !h.validateMultipartSessionActor(w, r, userID, session) {
		return
	}
	if err := multipartStore.AbortMultipartUpload(r.Context(), session.ObjectKey, session.UploadID); err != nil {
		slog.Error("failed to abort multipart upload", "error", err)
		writeError(w, http.StatusBadGateway, "failed to abort multipart upload")
		return
	}
	if err := h.Queries.MarkAttachmentUploadSessionAborted(r.Context(), db.MarkAttachmentUploadSessionAbortedParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		slog.Error("failed to mark upload session aborted", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to abort upload session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		http.Redirect(
			w,
			r,
			h.CFSigner.SignedURLWithContentDisposition(
				h.Storage.RemapURL(att.Url),
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
	if _, err := io.Copy(w, reader); err != nil {
		slog.Error("failed to stream attachment download", "id", uuidToString(att.ID), "error", err)
	}
}

// ---------------------------------------------------------------------------
// GetAttachmentPreviewURL — GET /api/attachments/{id}/preview-url
// Returns a short-lived presigned URL for previewing/downloading the attachment.
// ---------------------------------------------------------------------------

type previewURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

func (h *Handler) GetAttachmentPreviewURL(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}

	signed := h.signedPreviewURL(att)
	expires := time.Now().Add(h.attachmentDownloadURLTTL()).Unix()
	writeJSON(w, http.StatusOK, previewURLResponse{URL: signed, ExpiresAt: expires})
}

func (h *Handler) signedPreviewURL(att db.Attachment) string {
	if h.Storage == nil {
		return att.Url
	}
	if h.CFSigner != nil {
		cdnURL := h.Storage.RemapURL(att.Url)
		return h.CFSigner.SignedURL(cdnURL, time.Now().Add(h.attachmentDownloadURLTTL()))
	}
	// CDN domain set without CloudFront signer: public-read CDN; just remap.
	if h.Storage.CdnDomain() != "" {
		return h.Storage.RemapURL(att.Url)
	}
	key := h.Storage.KeyFromURL(att.Url)
	if key == "" {
		return att.Url
	}
	if ps, ok := h.Storage.(storage.PresignedInlineGetStorage); ok {
		if signed, err := ps.PresignedInlineGetURL(context.Background(), key, att.ContentType, att.Filename, h.attachmentDownloadURLTTL()); err == nil {
			return signed
		}
	}
	if ps, ok := h.Storage.(storage.PresignedGetStorage); ok {
		if signed, err := ps.PresignedGetURL(context.Background(), key, h.attachmentDownloadURLTTL()); err == nil {
			return signed
		}
	}
	if ps, ok := h.Storage.(storage.Presigner); ok {
		if signed, err := ps.PresignGet(context.Background(), key, h.attachmentDownloadURLTTL()); err == nil {
			return signed
		}
	}
	return h.Storage.RemapURL(att.Url)
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

	store := h.storageForURL(att.Url)
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}
	key := store.KeyFromURL(att.Url)
	reader, err := store.GetReader(r.Context(), key)
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

	h.deleteAttachmentObject(r.Context(), att.Url)
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

func (h *Handler) storageForURL(url string) storage.Storage {
	if h.LegacyLocalStorage != nil && h.LegacyLocalStorage.HandlesURL(url) {
		return h.LegacyLocalStorage
	}
	return h.Storage
}

// deleteAttachmentObject removes a single file by its stored URL.
func (h *Handler) deleteAttachmentObject(ctx context.Context, url string) {
	store := h.storageForURL(url)
	if store == nil || url == "" {
		return
	}
	store.Delete(ctx, store.KeyFromURL(url))
}

// deleteAttachmentObjects removes multiple files by their stored URLs.
func (h *Handler) deleteAttachmentObjects(ctx context.Context, urls []string) {
	if len(urls) == 0 {
		return
	}
	groupedKeys := map[storage.Storage][]string{}
	for _, u := range urls {
		store := h.storageForURL(u)
		if store == nil || u == "" {
			continue
		}
		groupedKeys[store] = append(groupedKeys[store], store.KeyFromURL(u))
	}
	for store, keys := range groupedKeys {
		store.DeleteKeys(ctx, keys)
	}
}

// deleteS3Object removes a single file from storage by its stored URL.
func (h *Handler) deleteS3Object(ctx context.Context, url string) {
	h.deleteAttachmentObject(ctx, url)
}

// deleteS3Objects removes multiple files from storage by their stored URLs.
func (h *Handler) deleteS3Objects(ctx context.Context, urls []string) {
	h.deleteAttachmentObjects(ctx, urls)
}

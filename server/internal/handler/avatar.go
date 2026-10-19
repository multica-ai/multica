package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Persist raw storage URLs as durable avatar references. At read time, private
// objects become stable /api/avatars/<sig>/<key> URLs that resolve through the
// deployment's download policy. Never persist expiring signatures.
//
// The endpoint uses its HMAC as the credential so native image loads work across
// origins without an Authorization header. Only image keys belonging to standalone,
// unbound uploads are publishable; issue, comment, chat, and task attachments are
// excluded. Enforce this at write time and on every read so old rows are covered
// and later attachment binding revokes access.
//
// Uploader identity is not required because agent copies reuse existing avatars.
// Unbound uploads are not exposed through attachment listing endpoints.

const avatarURLPathPrefix = "/api/avatars/"

// workspaceUploadNamespace is the storage prefix under which workspace-scoped
// content lives — issue/comment/chat attachments, avatars, and channel media
// ingest alike (see UploadFile and lark's mediaObjectKey). It is the only
// namespace where an object can belong to somebody other than whoever is
// setting the avatar, so it is the only one that has to prove itself.
const workspaceUploadNamespace = "workspaces/"

// avatarRedirectMaxAgeCap caps how long a client may reuse the 302 to a signed
// storage URL. The effective value is also clamped against
// attachmentDownloadURLTTL by avatarRedirectMaxAge so a cached redirect can
// never outlive the signature it points at, however short the operator sets
// the TTL.
const avatarRedirectMaxAgeCap = 60

// avatarProxyMaxAge is the cache lifetime for a proxied avatar body. Avatars
// are immutable per key (every upload mints a fresh UUIDv7 key), so this is
// only bounded by how quickly a re-upload should win.
const avatarProxyMaxAge = 300

// avatarExtContentTypes is the allowlist of avatar object types.
//
// SVG is deliberately absent. In proxy mode the body is served from the API
// origin, and an inline image/svg+xml document is a script-execution vector
// on navigation — the attachment proxy avoids it by forcing a download
// disposition, which an avatar cannot do and still render. An SVG avatar
// keeps its existing (pre-fix) behavior: the raw storage URL, which works on
// every public-bucket deployment.
var avatarExtContentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
}

var (
	avatarSigningKeyOnce sync.Once
	avatarSigningKey     []byte
)

// avatarURLSigningKey derives an avatar-specific HMAC key from JWT_SECRET via
// SHA-256, so this signing domain never shares an identical key with session
// tokens. Mirrors composioStateSecret in the router.
func avatarURLSigningKey() []byte {
	avatarSigningKeyOnce.Do(func() {
		sum := sha256.Sum256(append([]byte("avatar-url:"), auth.JWTSecret()...))
		avatarSigningKey = sum[:]
	})
	return avatarSigningKey
}

func signAvatarKey(key string) string {
	mac := hmac.New(sha256.New, avatarURLSigningKey())
	mac.Write([]byte(key))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func avatarKeySignatureValid(key, sig string) bool {
	return hmac.Equal([]byte(sig), []byte(signAvatarKey(key)))
}

// avatarContentType returns the Content-Type for an avatar storage key, or ""
// when the key is not an allowed image. Extension-based on purpose: the key is
// all this endpoint has, and it must decide before touching storage.
func avatarContentType(key string) string {
	return avatarExtContentTypes[strings.ToLower(path.Ext(key))]
}

// avatarURLPath builds the site-relative served URL for a storage key.
func avatarURLPath(key string) string {
	return avatarURLPathPrefix + signAvatarKey(key) + "/" + key
}

// avatarKeyFromServedURL recovers the storage key from a URL this server
// previously emitted, verifying the signature. Accepts both the site-relative
// and the PublicURL-absolute shape, because a client that round-trips a
// resolved response back into avatar_url can persist either.
func avatarKeyFromServedURL(raw string) (string, bool) {
	rest := raw
	if !strings.HasPrefix(rest, "/") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		rest = u.Path
	}
	rest, ok := strings.CutPrefix(rest, avatarURLPathPrefix)
	if !ok {
		return "", false
	}
	sig, key, ok := strings.Cut(rest, "/")
	if !ok || key == "" {
		return "", false
	}
	if !avatarKeySignatureValid(key, sig) {
		return "", false
	}
	return key, true
}

// resolveAvatarURL turns a stored avatar_url into a URL a browser can load.
//
// Passthrough (the common case — the value is returned untouched) when it is
// not one of our storage objects at all (an emoji marker, a data: URI, a
// Google/GitHub profile URL), when it is not an allowed image type, or when
// the deployment already serves the object publicly.
func (h *Handler) resolveAvatarURL(raw string) string {
	if raw == "" || h.Storage == nil {
		return raw
	}
	// Already one of ours (a client round-tripped a resolved response back
	// into avatar_url). Re-emit rather than nesting a second prefix; the
	// signature check keeps this from being a way to get an arbitrary key
	// signed.
	if key, ok := avatarKeyFromServedURL(raw); ok {
		return h.absolutizeAvatarPath(avatarURLPath(key))
	}
	key := h.ownedStorageKey(raw)
	if key == "" || avatarContentType(key) == "" {
		return raw
	}
	if h.avatarObjectLoadsUnauthenticated(raw) {
		return raw
	}
	return h.absolutizeAvatarPath(avatarURLPath(key))
}

// resolveAvatarURLPtr is resolveAvatarURL over the *string shape the response
// DTOs use. Nil and empty stay as they are so "no avatar" keeps its meaning.
func (h *Handler) resolveAvatarURLPtr(raw *string) *string {
	if raw == nil || *raw == "" {
		return raw
	}
	resolved := h.resolveAvatarURL(*raw)
	if resolved == *raw {
		return raw
	}
	return &resolved
}

// normalizeStoredAvatarURL is the write-side inverse: a client that PATCHes
// back a resolved `/api/avatars/...` value gets the durable object reference
// stored instead. Keeps the column's invariant — avatar_url holds a storage
// object URL — so a JWT_SECRET rotation can never strand an avatar behind a
// signature the server no longer accepts.
func (h *Handler) normalizeStoredAvatarURL(raw string) string {
	if h.Storage == nil {
		return raw
	}
	if key, ok := avatarKeyFromServedURL(raw); ok {
		return h.Storage.ObjectURL(key)
	}
	return raw
}

// acceptAvatarURL validates a client-supplied avatar_url and returns the value
// to persist. It writes the error response and returns ok=false when the
// caller must abort — same shape as loadIssueForUser and friends.
//
// `current` is the entity's stored avatar_url. An unchanged re-send is
// accepted without re-validation: clients round-trip whole objects, and a
// value that is already persisted grants nothing new.
func (h *Handler) acceptAvatarURL(w http.ResponseWriter, r *http.Request, raw, current string) (string, bool) {
	value := h.normalizeStoredAvatarURL(strings.TrimSpace(raw))
	if h.Storage == nil || value == strings.TrimSpace(current) {
		return value, true
	}
	// Not one of our storage objects (emoji marker, data: URI, third-party
	// profile URL) — nothing to authorize, and nothing this endpoint will
	// ever sign.
	key := h.ownedStorageKey(value)
	if key == "" {
		return value, true
	}
	if !h.avatarKeyIsPublishable(r.Context(), key) {
		writeError(w, http.StatusForbidden, "avatar_url must reference a standalone image upload, not a file attached to an issue, comment, or chat")
		return "", false
	}
	return value, true
}

// avatarKeyIsPublishable reports whether a storage object may be served
// through the public avatar endpoint. See the file header for why this is the
// authorization boundary rather than a mere sanity check.
//
// The key's basename is the attachment id: UploadFile mints one UUIDv7 and
// uses it as both the row id and the object filename, so this resolves the
// backing row without a lookup by URL (and without an index on it).
func (h *Handler) avatarKeyIsPublishable(ctx context.Context, key string) bool {
	if avatarContentType(key) == "" {
		return false
	}
	if !strings.HasPrefix(key, workspaceUploadNamespace) {
		// Outside the workspace namespace nothing can hold another user's
		// content: UploadFile only ever writes `workspaces/…` or
		// `users/<uploader>/…`, and the per-user branch creates no bindings.
		// Any other prefix is an object the operator put in the bucket
		// themselves, which they are entitled to point an avatar at.
		return true
	}
	attID, ok := attachmentIDFromStorageKey(key)
	if !ok {
		// Inside the workspace namespace but not an upload row — channel
		// media ingest writes `workspaces/<ws>/lark/…` keys here too. Fail
		// closed.
		return false
	}
	att, err := h.Queries.GetAttachmentByIDOnly(ctx, attID)
	if err != nil {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(att.ContentType), "image/") {
		return false
	}
	return !attachmentIsBound(att)
}

// attachmentIsBound reports whether an upload is attached to workspace content.
// A bound row is somebody's issue/comment/chat file — never an avatar.
func attachmentIsBound(att db.Attachment) bool {
	return att.IssueID.Valid ||
		att.CommentID.Valid ||
		att.ChatSessionID.Valid ||
		att.ChatMessageID.Valid ||
		att.TaskID.Valid
}

// attachmentIDFromStorageKey recovers the attachment id UploadFile embedded in
// the object filename (`<prefix>/<uuid><ext>`).
func attachmentIDFromStorageKey(key string) (pgtype.UUID, bool) {
	base := path.Base(key)
	stem := strings.TrimSuffix(base, path.Ext(base))
	id, err := util.ParseUUID(stem)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}

// ownedStorageKey returns the storage key for rawURL when — and only when —
// this deployment's storage backend is what produced that URL.
//
// The round-trip through ObjectURL is what makes this safe: KeyFromURL is
// lossy by design (it falls back to "everything after the last slash" for
// unrecognized inputs), so a Google or GitHub avatar URL would otherwise
// yield a plausible-looking key. Re-deriving the URL from the key and
// requiring an exact match rejects anything this storage did not mint.
func (h *Handler) ownedStorageKey(rawURL string) string {
	key := h.Storage.KeyFromURL(rawURL)
	if key == "" || key == rawURL {
		return ""
	}
	if h.Storage.ObjectURL(key) != rawURL {
		return ""
	}
	return key
}

// avatarObjectLoadsUnauthenticated reports whether the raw storage URL is
// already loadable by an unauthenticated browser fetch, in which case
// rewriting it would only add a pointless hop through the API.
func (h *Handler) avatarObjectLoadsUnauthenticated(rawURL string) bool {
	// LocalStorage objects are served by the public /uploads/* route, whether
	// the stored URL is site-relative or absolute via LOCAL_UPLOAD_BASE_URL.
	if _, ok := h.Storage.(*storage.LocalStorage); ok {
		return true
	}
	// Otherwise: a public CDN domain with no per-request CloudFront signing.
	// In signed-CloudFront mode the same domain serves private content and the
	// unsigned URL is a 403, so that shape resolves through the endpoint too.
	return h.storageURLIsPubliclyReadable(rawURL)
}

// absolutizeAvatarPath anchors the served path on MULTICA_PUBLIC_URL when it
// is configured, so clients that don't share the API's document origin
// (Desktop, mobile webview) can load it. Same policy as buildMarkdownURL;
// falling back to the site-relative path is safe because every client
// resolves avatar URLs through resolvePublicFileUrl, which prefixes its API
// base URL.
func (h *Handler) absolutizeAvatarPath(relPath string) string {
	if publicURL := strings.TrimRight(h.cfg.PublicURL, "/"); publicURL != "" {
		return publicURL + relPath
	}
	return relPath
}

// ServeAvatar — GET /api/avatars/{sig}/*
//
// Unauthenticated by design (the signature is the credential) — see the file
// header. Resolution reuses resolveAttachmentDownloadMode so an avatar and an
// attachment obey the same ATTACHMENT_DOWNLOAD_MODE policy on a given
// deployment; there is one storage backend and one right way to read from it.
func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		http.NotFound(w, r)
		return
	}

	key, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil || key == "" {
		http.NotFound(w, r)
		return
	}
	contentType := avatarContentType(key)
	// Deny shape is a flat 404 for a bad signature, an unknown key, a
	// non-image key, and an object that is not avatar-class alike, so the
	// route is not an oracle for any of them. The publishable check runs on
	// every request rather than only at write time: it is what bounds rows
	// written before the write-side gate existed, and it revokes the URL if
	// the object is later bound to a comment or chat.
	if contentType == "" ||
		!avatarKeySignatureValid(key, chi.URLParam(r, "sig")) ||
		!h.avatarKeyIsPublishable(r.Context(), key) {
		http.NotFound(w, r)
		return
	}

	objectURL := h.Storage.ObjectURL(key)
	h.setAttachmentPreviewSecurityHeaders(w)

	switch h.resolveAttachmentDownloadMode(objectURL) {
	case attachmentDownloadModeCloudFront:
		if h.CFSigner == nil {
			writeError(w, http.StatusInternalServerError, "cloudfront avatar downloads are not configured")
			return
		}
		setAvatarCacheControl(w, h.avatarRedirectMaxAge())
		http.Redirect(w, r, h.CFSigner.SignedURL(objectURL, time.Now().Add(h.attachmentDownloadURLTTL())), http.StatusFound)
	case attachmentDownloadModePresign:
		presigner, ok := h.Storage.(storage.DownloadPresigner)
		if !ok {
			writeError(w, http.StatusInternalServerError, "avatar storage does not support presigned downloads")
			return
		}
		// Empty disposition inherits the object's stored Content-Disposition
		// (inline for images), which is what keeps the redirect renderable in
		// an <img> instead of triggering a download.
		signedURL, err := presigner.PresignGetWithContentDisposition(r.Context(), key, h.attachmentDownloadURLTTL(), "")
		if err != nil {
			slog.Error("failed to presign avatar", "key", key, "error", err)
			writeError(w, http.StatusBadGateway, "failed to create avatar URL")
			return
		}
		setAvatarCacheControl(w, h.avatarRedirectMaxAge())
		http.Redirect(w, r, signedURL, http.StatusFound)
	case attachmentDownloadModeProxy:
		h.proxyAvatar(w, r, key, contentType)
	default:
		writeError(w, http.StatusInternalServerError, "invalid attachment download mode")
	}
}

// proxyAvatar streams the object through the API for backends that cannot be
// reached directly by the browser (local disk, or a private host such as
// http://rustfs:9000). No Range handling: avatars are small images loaded by
// <img>, not resumable downloads.
func (h *Handler) proxyAvatar(w http.ResponseWriter, r *http.Request, key, contentType string) {
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Warn("avatar object not found", "key", key, "error", err)
		http.NotFound(w, r)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	setAvatarCacheControl(w, avatarProxyMaxAge)

	if _, err := io.Copy(w, reader); err != nil {
		slog.Warn("avatar stream interrupted", "key", key, "error", err)
	}
}

// avatarRedirectMaxAge is how long a client may reuse the 302, clamped to half
// the signed URL's own lifetime. ATTACHMENT_DOWNLOAD_URL_TTL accepts any
// positive duration, so a fixed 60s would let a browser or proxy keep replaying
// a redirect to an already-expired storage URL on a deployment that configures
// a shorter TTL. Returns 0 (no caching at all) when the TTL is too short for
// any margin to be safe.
func (h *Handler) avatarRedirectMaxAge() int {
	ttlSeconds := int(h.attachmentDownloadURLTTL() / time.Second)
	maxAge := ttlSeconds / 2
	if maxAge > avatarRedirectMaxAgeCap {
		return avatarRedirectMaxAgeCap
	}
	if maxAge < 0 {
		return 0
	}
	return maxAge
}

// setAvatarCacheControl marks the response private: the URL is unguessable
// but it is still a per-deployment identity image, and a shared proxy has no
// business holding a copy. A zero max-age means "do not store at all" rather
// than "store for zero seconds", which some intermediaries round up.
func setAvatarCacheControl(w http.ResponseWriter, maxAge int) {
	if maxAge <= 0 {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
}

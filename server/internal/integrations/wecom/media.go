package wecom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// Bounds on WeCom media ingest (spec §5.6). maxMediaCiphertextBytes is the
// plaintext cap (100 MiB) plus one full mediaPadBlock of PKCS#7 padding — the
// wire ciphertext can legitimately be up to that many bytes larger than the
// plaintext it decrypts to. The slack is 32, not one 16-byte AES block: WeCom
// pads to a 32-byte block (see mediaPadBlock in crypto.go). maxMediaRedirects is the max number of redirect
// hops followed, re-validating the destination on every hop (dialAllowedAddr
// runs on every new connection, including each redirect target).
// mimeSniffBytes is how much of the DECRYPTED plaintext is sniffed for MIME
// detection (never the ciphertext, which sniffs as random bytes).
const (
	maxMediaCiphertextBytes int64 = 100<<20 + mediaPadBlock
	maxMediaRedirects             = 3
	defaultMediaConcurrency       = 2
	mimeSniffBytes                = 512
)

// Sentinel errors surfaced by the media pipeline. Wrapped errors from the
// HTTP layer are deliberately NOT included in these (see sanitizeMediaErr) —
// a raw *url.Error or dial error can embed the short-lived, credential-
// bearing WeCom media URL, which must never reach a log line or last_error
// (spec §5.6 item 6).
var (
	// ErrMediaTooLarge is returned when the declared or observed ciphertext
	// size exceeds maxMediaCiphertextBytes.
	ErrMediaTooLarge = errors.New("wecom: media exceeds the configured size limit")
	// ErrMediaSchemeNotAllowed is returned for a non-HTTPS media URL, on
	// the initial request or on any redirect hop.
	ErrMediaSchemeNotAllowed = errors.New("wecom: media url must use https")
	// ErrMediaHostNotAllowed is returned when a media host resolves to a
	// loopback, link-local, private, or other disallowed address.
	ErrMediaHostNotAllowed = errors.New("wecom: media host resolves to a disallowed address")
	// ErrMediaFetchFailed covers HTTP-layer failures (network error,
	// non-200 status). The underlying error is never wrapped in directly
	// because it may carry the media URL.
	ErrMediaFetchFailed = errors.New("wecom: media fetch failed")
	// ErrTooManyMediaRedirects is returned once a download exceeds
	// maxMediaRedirects hops.
	ErrTooManyMediaRedirects = errors.New("wecom: media download exceeded the redirect limit")
)

// mediaStorage is the narrow upload surface media.go needs from
// storage.Storage — mirrors lark's media_ingest.go so both adapters depend
// on the same small seam rather than the full storage interface.
type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	// ObjectURL is the URL a successful upload of key returns — a pure
	// function of configuration, so the intent ledger can persist it
	// BEFORE the PUT.
	ObjectURL(key string) string
}

// mediaStreamStorage is implemented by storage backends (S3Storage,
// LocalStorage) that can accept a streamed upload with a known size,
// avoiding a full in-memory buffer of the plaintext.
type mediaStreamStorage interface {
	UploadStream(ctx context.Context, key string, data io.Reader, sizeBytes int64, contentType string, filename string) (string, error)
}

// MediaResolverConfig wires the WeCom engine.MediaResolver's dependencies.
type MediaResolverConfig struct {
	Storage mediaStorage
	Ledger  engine.MediaIntentLedger
	Logger  *slog.Logger
	// HTTPClient overrides the default SSRF-hardened client. Production
	// callers should leave this nil so downloads go through
	// newSecureMediaClient's HTTPS + redirect + private-IP dial
	// validation; tests inject an httptest client to exercise the
	// download/decrypt/upload pipeline without a real network. Regardless
	// of override, NewMediaResolver always installs its own CheckRedirect
	// (scheme + hop-count policy) on the client.
	HTTPClient *http.Client
	// Concurrency overrides the default per-process download cap (2,
	// spec §5.6 item 3). It sits underneath the engine's own global media
	// semaphore and exists to bound peak temp-file disk usage specifically
	// for this adapter. Zero uses defaultMediaConcurrency.
	Concurrency int
	// TempDir overrides the directory ciphertext/plaintext temp files are
	// created in (os.CreateTemp default when empty). Tests use this to
	// assert no temp file outlives ResolveMedia.
	TempDir string
	// MaxCiphertextBytes overrides maxMediaCiphertextBytes. Tests use a
	// small value to exercise the oversize path without transferring 100
	// MiB. Zero or negative uses the production default.
	MaxCiphertextBytes int64
}

type mediaResolver struct {
	client        *http.Client
	storage       mediaStorage
	ledger        engine.MediaIntentLedger
	logger        *slog.Logger
	sem           chan struct{}
	tempDir       string
	maxCiphertext int64
}

var _ engine.MediaResolver = (*mediaResolver)(nil)

// NewMediaResolver builds the WeCom engine.MediaResolver: bounded HTTPS
// download, AES-256-CBC + PKCS#7 decrypt through 0600 temp files, MIME
// sniff, and stream upload (spec §5.6).
func NewMediaResolver(cfg MediaResolverConfig) engine.MediaResolver {
	client := cfg.HTTPClient
	if client == nil {
		client = newSecureMediaClient()
	}
	// Always installed, even on an injected test client: scheme and
	// hop-count policy must hold regardless of how the client reaches the
	// network.
	client.CheckRedirect = mediaCheckRedirect

	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = defaultMediaConcurrency
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxCiphertext := cfg.MaxCiphertextBytes
	if maxCiphertext <= 0 {
		maxCiphertext = maxMediaCiphertextBytes
	}
	return &mediaResolver{
		client:        client,
		storage:       cfg.Storage,
		ledger:        cfg.Ledger,
		logger:        logger,
		sem:           make(chan struct{}, concurrency),
		tempDir:       cfg.TempDir,
		maxCiphertext: maxCiphertext,
	}
}

// HasMedia is a pure in-memory check (no I/O): it reports whether the
// inbound callback carried at least one image/file/video reference,
// including images embedded in a mixed message (spec §5.6).
func (r *mediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeWecomRaw(msg)
	if err != nil {
		return false
	}
	return len(raw.Media) > 0
}

// ResolveMedia downloads, decrypts, sniffs, and uploads every media
// reference on msg, appending a channel.MediaRef per success. Failures are
// best-effort per item: the caller keeps whatever placeholder text
// inbound.go already produced and nothing is deleted on a partial failure
// (the intent ledger + reconciler own that).
func (r *mediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeWecomRaw(msg)
	if err != nil || len(raw.Media) == 0 {
		return msg
	}
	if r.storage == nil || r.ledger == nil {
		r.warn(msg.MessageID, "", "media ingest skipped: missing dependency", nil)
		return msg
	}
	for i, item := range raw.Media {
		if ctx.Err() != nil {
			break
		}
		if ref, ok := r.resolveOne(ctx, inst, chatMessageID, msg.MessageID, i, item); ok {
			msg.MediaRefs = append(msg.MediaRefs, ref)
		}
	}
	return msg
}

func (r *mediaResolver) resolveOne(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int, item wecomRawMediaItem) (channel.MediaRef, bool) {
	msgType, ok := wecomMediaMsgType(item.MsgType)
	if !ok {
		return channel.MediaRef{}, false
	}
	if err := validateHTTPSURL(item.URL); err != nil {
		r.warn(messageID, item.MsgType, "media download skipped: url rejected", err)
		return channel.MediaRef{}, false
	}
	key, err := decodeAESKey(item.AESKey)
	if err != nil {
		r.warn(messageID, item.MsgType, "media download skipped: aeskey decode failed", err)
		return channel.MediaRef{}, false
	}

	// Intent MUST be durable before any object write (engine.MediaIntentLedger
	// doc): every failure from here on just leaves this row for the
	// reconciler, and nothing already uploaded is ever deleted inline.
	storageKey := wecomMediaObjectKey(inst, chatMessageID, messageID, index)
	objectURL := r.storage.ObjectURL(storageKey)
	recorded, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{
		StorageKey:     storageKey,
		WorkspaceID:    inst.WorkspaceID,
		ChatMessageID:  chatMessageID,
		StorageURL:     objectURL,
		InstallationID: inst.ID,
	})
	if err != nil {
		r.warn(messageID, item.MsgType, "media download skipped: intent record failed", err)
		return channel.MediaRef{}, false
	}
	if !recorded {
		// The reconciler owns this key ('deleting'); never resurrect it.
		r.warn(messageID, item.MsgType, "media download skipped: key owned by reconciler", nil)
		return channel.MediaRef{}, false
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return channel.MediaRef{}, false
	}

	plainPath, sizeBytes, mimeType, cleanup, err := r.downloadDecrypt(ctx, item.URL, key)
	defer cleanup()
	if err != nil {
		r.warn(messageID, item.MsgType, "media download or decrypt failed", err)
		return channel.MediaRef{}, false
	}

	filename := wecomMediaFilename(item, msgType, mimeType, messageID, index)
	uploadedURL, err := r.upload(ctx, plainPath, storageKey, sizeBytes, mimeType, filename)
	if err != nil {
		r.warn(messageID, item.MsgType, "media upload failed", err)
		return channel.MediaRef{}, false
	}

	return channel.MediaRef{
		Type:       msgType,
		StorageKey: storageKey,
		StorageURL: uploadedURL,
		Filename:   filename,
		MimeType:   mimeType,
		SizeBytes:  sizeBytes,
	}, true
}

// downloadDecrypt streams ciphertext into a 0600 temp file, then streams its
// AES-256-CBC + PKCS#7 decrypt into a second 0600 temp file, sniffing the
// first mimeSniffBytes of the PLAINTEXT for its content type. The returned
// cleanup always removes both temp files and must be called on every exit
// path (spec §5.6 item 3); it is never nil.
func (r *mediaResolver) downloadDecrypt(ctx context.Context, rawURL string, key []byte) (plainPath string, sizeBytes int64, mimeType string, cleanup func(), err error) {
	cleanup = func() {}

	cipherFile, err := os.CreateTemp(r.tempDir, "wecom-media-cipher-*")
	if err != nil {
		return "", 0, "", cleanup, fmt.Errorf("wecom: create ciphertext temp file: %w", err)
	}
	cipherPath := cipherFile.Name()
	cleanup = func() { _ = os.Remove(cipherPath) }

	fetchErr := r.fetchToFile(ctx, rawURL, cipherFile)
	closeErr := cipherFile.Close()
	if fetchErr != nil {
		return "", 0, "", cleanup, fetchErr
	}
	if closeErr != nil {
		return "", 0, "", cleanup, fmt.Errorf("wecom: close ciphertext temp file: %w", closeErr)
	}

	plainFile, err := os.CreateTemp(r.tempDir, "wecom-media-plain-*")
	if err != nil {
		return "", 0, "", cleanup, fmt.Errorf("wecom: create plaintext temp file: %w", err)
	}
	// plainFilePath, not the named return plainPath, is what cleanup closes
	// over: plainPath gets reset to "" by every `return "", ...` below (Go
	// assigns to named returns before the deferred/returned closure ever
	// runs), which would make a later cleanup() call os.Remove("") and leak
	// this temp file on error paths.
	plainFilePath := plainFile.Name()
	plainPath = plainFilePath
	cleanup = func() {
		_ = os.Remove(cipherPath)
		_ = os.Remove(plainFilePath)
	}

	cipherReader, err := os.Open(cipherPath)
	if err != nil {
		_ = plainFile.Close()
		return "", 0, "", cleanup, fmt.Errorf("wecom: reopen ciphertext temp file: %w", err)
	}
	written, decErr := decryptFromReader(key, cipherReader, plainFile)
	_ = cipherReader.Close()
	if decErr != nil {
		_ = plainFile.Close()
		return "", 0, "", cleanup, decErr
	}
	if err := plainFile.Sync(); err != nil {
		_ = plainFile.Close()
		return "", 0, "", cleanup, fmt.Errorf("wecom: sync plaintext temp file: %w", err)
	}
	if _, err := plainFile.Seek(0, io.SeekStart); err != nil {
		_ = plainFile.Close()
		return "", 0, "", cleanup, fmt.Errorf("wecom: seek plaintext temp file: %w", err)
	}
	sniffBuf := make([]byte, mimeSniffBytes)
	n, readErr := plainFile.Read(sniffBuf)
	if readErr != nil && readErr != io.EOF {
		_ = plainFile.Close()
		return "", 0, "", cleanup, fmt.Errorf("wecom: read plaintext for mime sniff: %w", readErr)
	}
	mimeType = http.DetectContentType(sniffBuf[:n])
	if err := plainFile.Close(); err != nil {
		return "", 0, "", cleanup, fmt.Errorf("wecom: close plaintext temp file: %w", err)
	}
	return plainPath, written, mimeType, cleanup, nil
}

// fetchToFile performs the bounded HTTPS GET and copies AT MOST
// maxMediaCiphertextBytes+1 bytes into dst — the +1 lets us detect and
// reject an oversize body even when the server lied about or omitted
// Content-Length, without ever buffering the whole thing.
func (r *mediaResolver) fetchToFile(ctx context.Context, rawURL string, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ErrMediaFetchFailed
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return ErrMediaFetchFailed
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrMediaFetchFailed, resp.StatusCode)
	}
	if resp.ContentLength > 0 && resp.ContentLength > r.maxCiphertext {
		return ErrMediaTooLarge
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, r.maxCiphertext+1))
	if err != nil {
		return fmt.Errorf("wecom: read media body: %w", err)
	}
	if n > r.maxCiphertext {
		return ErrMediaTooLarge
	}
	return nil
}

func (r *mediaResolver) upload(ctx context.Context, plainPath, key string, sizeBytes int64, mimeType, filename string) (string, error) {
	f, err := os.Open(plainPath)
	if err != nil {
		return "", fmt.Errorf("wecom: open plaintext for upload: %w", err)
	}
	defer func() { _ = f.Close() }()
	if streamStorage, ok := r.storage.(mediaStreamStorage); ok && sizeBytes > 0 {
		return streamStorage.UploadStream(ctx, key, f, sizeBytes, mimeType, filename)
	}
	// Unknown/zero-length or a non-streaming backend: buffer. The transport
	// already caps ciphertext at ~100 MiB, so plaintext is bounded too.
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("wecom: read plaintext for upload: %w", err)
	}
	return r.storage.Upload(ctx, key, data, mimeType, filename)
}

func (r *mediaResolver) warn(messageID, wireMsgType, msg string, err error) {
	if r.logger == nil {
		return
	}
	args := []any{"message_id", messageID}
	if wireMsgType != "" {
		args = append(args, "msgtype", wireMsgType)
	}
	if err != nil {
		args = append(args, "err", sanitizeMediaErr(err))
	}
	r.logger.Warn("wecom: "+msg, args...)
}

// sanitizeMediaErr strips sensitive paths from errors before they reach a
// log line: *url.Error embeds the request URL (a short-lived WeCom media
// credential), and *fs.PathError / *os.LinkError embed local temp-file
// paths from os.CreateTemp/Open/Close/Sync/Seek/Read (spec §5.6 item 6 —
// aeskey, URL, temp paths, and plaintext must never be logged).
func sanitizeMediaErr(err error) error {
	if err == nil {
		return nil
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return sanitizeMediaErr(uerr.Err)
	}
	var perr *fs.PathError
	if errors.As(err, &perr) {
		sanitized := sanitizePathError(perr)
		if err == perr {
			return sanitized
		}
		return rewrapSanitizedMediaErr(err, perr, sanitized)
	}
	var lerr *os.LinkError
	if errors.As(err, &lerr) {
		sanitized := sanitizeLinkError(lerr)
		if err == lerr {
			return sanitized
		}
		return rewrapSanitizedMediaErr(err, lerr, sanitized)
	}
	return err
}

func sanitizePathError(perr *fs.PathError) error {
	if perr.Err != nil {
		return fmt.Errorf("%s: %w", perr.Op, sanitizeMediaErr(perr.Err))
	}
	return fmt.Errorf("%s failed", perr.Op)
}

func sanitizeLinkError(lerr *os.LinkError) error {
	if lerr.Err != nil {
		return fmt.Errorf("%s: %w", lerr.Op, sanitizeMediaErr(lerr.Err))
	}
	return fmt.Errorf("%s failed", lerr.Op)
}

func rewrapSanitizedMediaErr(outer, inner, sanitizedInner error) error {
	outerStr := outer.Error()
	innerStr := inner.Error()
	if strings.HasSuffix(outerStr, innerStr) {
		prefix := strings.TrimSuffix(outerStr, innerStr)
		prefix = strings.TrimSuffix(prefix, ": ")
		if prefix != "" {
			return fmt.Errorf("%s: %w", prefix, sanitizedInner)
		}
		return sanitizedInner
	}
	return sanitizedInner
}

func validateHTTPSURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrMediaSchemeNotAllowed
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrMediaSchemeNotAllowed
	}
	if u.Scheme != "https" || u.Host == "" {
		return ErrMediaSchemeNotAllowed
	}
	return nil
}

// mediaCheckRedirect enforces the HTTPS-only + max-3-redirect policy (spec
// §5.6 item 1) on every hop, whether the client is the default secure one
// or a test-injected override.
//
// net/http calls CheckRedirect with via = the requests already sent, NOT
// including the one about to be sent to follow the current redirect. So
// via has length 1 before following the FIRST redirect, length 2 before
// the second, and so on — allowing exactly maxMediaRedirects redirects
// means rejecting once len(via) > maxMediaRedirects (i.e. we are about to
// follow the (maxMediaRedirects+1)-th one), not >=.
func mediaCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxMediaRedirects {
		return ErrTooManyMediaRedirects
	}
	if req.URL.Scheme != "https" {
		return ErrMediaSchemeNotAllowed
	}
	return nil
}

// newSecureMediaClient builds the production HTTP client: HTTPS + redirect
// policy via CheckRedirect (installed by the caller, NewMediaResolver), and
// SSRF protection via a DialContext override that resolves the destination
// itself and rejects loopback/link-local/private/reserved addresses BEFORE
// connecting — re-run on every dial, so every redirect hop (a new
// connection, since it is normally a different host) is re-validated
// against the current DNS answer rather than trusting the URL's hostname
// literal (spec §5.6 item 1: "每次重定向重新校验目标").
func newSecureMediaClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialAllowedMediaAddr(ctx, dialer, network, addr)
	}
	return &http.Client{Transport: transport}
}

func dialAllowedMediaAddr(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error = ErrMediaHostNotAllowed
	for _, ipAddr := range ips {
		if !isAllowedMediaIP(ipAddr.IP) {
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

// isAllowedMediaIP rejects loopback, link-local, private (RFC 1918 / RFC
// 4193), unspecified, and multicast addresses, plus the IPv4 shared address
// space (100.64.0.0/10, RFC 6598 — CGNAT ranges routinely used for internal
// infra and NOT covered by any net.IP predicate).
func isAllowedMediaIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xC0 == 64 {
		return false
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() {
		return false
	}
	return true
}

// wecomMediaMsgType maps a wire msgtype ("image" / "file" / "video") onto
// the normalized channel.MsgType. Any other value (defensive — inbound.go
// only ever populates Media with these three) is rejected.
func wecomMediaMsgType(wire string) (channel.MsgType, bool) {
	switch wire {
	case "image":
		return channel.MsgTypeImage, true
	case "file":
		return channel.MsgTypeFile, true
	case "video":
		return channel.MsgTypeVideo, true
	default:
		return channel.MsgTypeUnknown, false
	}
}

// wecomMediaObjectKey derives the object key from the durable chat_message
// the object will attach to, the platform message id, and the item's index
// within that message — NOT from the download URL (which is single-use and
// expires in 5 minutes) or any other value that would need logging to
// debug. Mirrors lark's mediaObjectKey: keying off chatMessageID keeps two
// ingests of a reclaimed/duplicate platform message from colliding on one
// ledger row.
func wecomMediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int) string {
	sum := sha256.Sum256([]byte(util.UUIDToString(chatMessageID) + "\x00" + messageID + "\x00" + strconv.Itoa(index)))
	return path.Join(
		"workspaces",
		util.UUIDToString(inst.WorkspaceID),
		"wecom",
		util.UUIDToString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
}

func wecomMediaFilename(item wecomRawMediaItem, msgType channel.MsgType, mimeType, messageID string, index int) string {
	if name := cleanMediaFilename(item.Filename); name != "" {
		return name
	}
	prefix := "wecom-file"
	switch msgType {
	case channel.MsgTypeImage:
		prefix = "wecom-image"
	case channel.MsgTypeVideo:
		prefix = "wecom-video"
	}
	return fmt.Sprintf("%s-%s-%d%s", prefix, safeMediaPathSegment(messageID), index, mediaExtensionForMIME(mimeType))
}

func cleanMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func safeMediaPathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func mediaExtensionForMIME(mimeType string) string {
	if semi := strings.IndexByte(mimeType, ';'); semi >= 0 {
		mimeType = strings.TrimSpace(mimeType[:semi])
	}
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

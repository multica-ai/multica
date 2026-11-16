package slack

// Slack media ingestion runs after a message has been accepted and persisted,
// keeping network and storage I/O off the connector acknowledgement path.
//
// HasMedia only inspects the translated event payload. ResolveMedia fetches
// each file and records a durable intent before uploading it, so failures or
// crashes leave enough state for the reconciler to remove unbound objects.
//
// Slack private file URLs require the installation's bot token as a bearer
// credential. The download client therefore accepts only HTTPS URLs on
// Slack-owned hosts, including every redirect destination.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Slack caps a message at 10 files; the byte cap keeps the buffered download
// path well inside the Router's 45-second media deadline and memory budget.
const (
	maxFilesPerMessage   = 10
	maxInboundFileBytes  = 20 << 20
	fileFetchTimeout     = 30 * time.Second
	maxDownloadRedirects = 3
)

// mediaStorage defines the object-store operations required for ingestion.
// ObjectURL must derive the final URL without performing I/O so the resolver
// can persist that URL in the intent ledger before uploading the object.
type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	ObjectURL(key string) string
}

type slackMediaResolver struct {
	decrypt Decrypter
	storage mediaStorage
	ledger  engine.MediaIntentLedger
	fetch   *http.Client
	logger  *slog.Logger
	// allowedHost gates which hosts may receive the bot token. Production is
	// always isSlackFileHost; tests substitute their httptest host.
	allowedHost func(host string) bool
}

var _ engine.MediaResolver = (*slackMediaResolver)(nil)

// NewMediaResolver builds the Slack media resolver. Storage and the intent
// ledger are both required; ResolveMedia logs and returns the original message
// unchanged if either dependency is unavailable.
func NewMediaResolver(decrypt Decrypter, storage mediaStorage, ledger engine.MediaIntentLedger, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	r := &slackMediaResolver{
		decrypt:     decrypt,
		storage:     storage,
		ledger:      ledger,
		logger:      logger,
		allowedHost: isSlackFileHost,
	}
	r.fetch = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxDownloadRedirects {
				return errors.New("too many redirects")
			}
			// Validate every redirect before net/http follows it. This host check
			// prevents redirected requests from leaving Slack's domains regardless
			// of how net/http propagates the Authorization header.
			if err := r.validateDownloadURL(req.URL); err != nil {
				return err
			}
			// net/http forwards Authorization only to the same host or a
			// subdomain of it, so a hop to a sibling such as
			// files-origin.slack.com arrives unauthenticated and Slack answers
			// with its HTML login page — indistinguishable from a missing
			// scope, and the file would be dropped. The check above already
			// proved this destination is Slack-owned, so restore the token.
			if auth := via[0].Header.Get("Authorization"); auth != "" {
				req.Header.Set("Authorization", auth)
			}
			return nil
		},
	}
	return r
}

// isSlackFileHost reports whether host is Slack's own. url_private URLs live
// on files.slack.com (enterprise variants are still *.slack.com subdomains).
func isSlackFileHost(host string) bool {
	host = strings.ToLower(host)
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}

// isFetchableSlackFileURL is the pure form of validateDownloadURL, pinned to
// the production host predicate instead of the resolver's test seam. The
// inbound translation uses it to drop files the resolver could never fetch
// before they ever reach the envelope.
func isFetchableSlackFileURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https") && isSlackFileHost(parsed.Hostname())
}

// HasMedia is a pure decode of the already-translated event payload. It runs
// synchronously on the connector ACK path, so no I/O.
func (r *slackMediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeSlackRaw(msg)
	return err == nil && len(raw.Files) > 0
}

// ResolveMedia downloads and stores every file on the message, returning it
// with a MediaRef per object that landed. Files are independent: one that
// fails does not stop the rest.
func (r *slackMediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeSlackRaw(msg)
	if err != nil || len(raw.Files) == 0 {
		return msg
	}
	if r.storage == nil || r.ledger == nil {
		r.logWarn(msg, errors.New("media dependency missing"))
		return msg
	}
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		r.logWarn(msg, errors.New("installation platform row unavailable"))
		return msg
	}
	creds, err := decodeCredentials(row.Config, r.decrypt)
	if err != nil {
		r.logWarn(msg, fmt.Errorf("decode credentials: %w", err))
		return msg
	}
	files := raw.Files
	if len(files) > maxFilesPerMessage {
		r.logWarn(msg, fmt.Errorf("%d files exceed the limit of %d; extra files skipped", len(files), maxFilesPerMessage))
		files = files[:maxFilesPerMessage]
	}
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			// The Router discards every ref once the media deadline passes, so
			// there is nothing left to win by starting another file.
			r.logWarn(msg, fmt.Errorf("media budget spent after %d of %d files: %w", i, len(files), err))
			break
		}
		fileCtx, cancel := context.WithTimeout(ctx, fileFetchBudget(ctx, len(files)-i))
		ref, err := r.ingestOne(fileCtx, inst, chatMessageID, i, f, creds.BotToken)
		cancel()
		if err != nil {
			r.logger.Warn("slack media ingest failed",
				"installation_id", util.UUIDToString(inst.ID),
				"message_id", msg.MessageID,
				"file", i,
				"err", err)
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}
	return msg
}

// fileFetchBudget caps one file's download and upload. The flat timeout bounds
// a single stalled transfer, but maxFilesPerMessage of them in series would run
// far past the Router's media deadline — and the Router drops EVERY ref once
// that deadline passes, so one slow file would cost the whole message its
// attachments. Sharing what is left of the budget over the files still to fetch
// keeps a slow early file from starving the rest; files that finish quickly
// hand their unused share back to the ones behind them.
func fileFetchBudget(ctx context.Context, remainingFiles int) time.Duration {
	if remainingFiles < 1 {
		remainingFiles = 1
	}
	budget := fileFetchTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if share := time.Until(deadline) / time.Duration(remainingFiles); share < budget {
			budget = share
		}
	}
	return budget
}

// ingestOne carries a single file from URL to MediaRef. The ledger row goes
// first: from that point on every failure — download, upload, a crash — leaves
// an intent the reconciler settles, and nothing here deletes anything.
func (r *slackMediaResolver) ingestOne(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, index int, f slackRawFile, botToken string) (channel.MediaRef, error) {
	// Slack states the size up front, so an oversized file is refused before it
	// costs an intent row and a full transfer. A file that under-reports still
	// hits the byte cap in download.
	if f.Size > maxInboundFileBytes {
		return channel.MediaRef{}, fmt.Errorf("file size %d exceeds the %d MiB limit", f.Size, maxInboundFileBytes>>20)
	}
	key := slackMediaObjectKey(inst, chatMessageID, f, index)
	link := r.storage.ObjectURL(key)
	owned, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{
		StorageKey:     key,
		WorkspaceID:    inst.WorkspaceID,
		ChatMessageID:  chatMessageID,
		StorageURL:     link,
		InstallationID: inst.ID,
	})
	if err != nil {
		// No durable intent, no upload — the fail-safe direction.
		return channel.MediaRef{}, fmt.Errorf("record media intent: %w", err)
	}
	if !owned {
		// The reconciler owns this key; never resurrect it.
		return channel.MediaRef{}, errors.New("media key owned by reconciler")
	}
	data, responseType, err := r.download(ctx, f.DownloadURL, botToken)
	if err != nil {
		return channel.MediaRef{}, err
	}
	contentType, err := slackFileContentType(f, responseType, data)
	if err != nil {
		return channel.MediaRef{}, err
	}
	filename := slackFileName(f, index, contentType)
	if _, err := r.storage.Upload(ctx, key, data, contentType, filename); err != nil {
		// The store may still be processing the PUT; the intent row covers the
		// object either way.
		return channel.MediaRef{}, fmt.Errorf("upload file: %w", err)
	}
	return channel.MediaRef{
		Type:       slackMediaKind(contentType),
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  int64(len(data)),
	}, nil
}

func (r *slackMediaResolver) download(ctx context.Context, rawURL, botToken string) ([]byte, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", errors.New("invalid file download URL")
	}
	if err := r.validateDownloadURL(parsed); err != nil {
		return nil, "", err
	}
	// The per-file budget is already on ctx (see fileFetchBudget), which also
	// covers the upload that follows.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", errors.New("build file download request failed")
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	resp, err := r.fetch.Do(req)
	if err != nil {
		// net/http's *url.Error echoes the full request URL; keep private
		// Slack file URLs out of the logs.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, "", fmt.Errorf("download file request failed: %w", urlErr.Err)
		}
		return nil, "", errors.New("download file request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download file: http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundFileBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read file: %w", err)
	}
	if len(data) > maxInboundFileBytes {
		return nil, "", fmt.Errorf("file exceeds the %d MiB limit", maxInboundFileBytes>>20)
	}
	contentType := resp.Header.Get("Content-Type")
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	return data, contentType, nil
}

func (r *slackMediaResolver) validateDownloadURL(parsed *url.URL) error {
	if parsed == nil || parsed.Host == "" || parsed.User != nil {
		return errors.New("invalid file download URL shape")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("invalid file download URL scheme %q", parsed.Scheme)
	}
	if !r.allowedHost(parsed.Hostname()) {
		return errors.New("blocked non-Slack file download host")
	}
	return nil
}

// slackFileContentType decides what the stored object is. An unauthorized or
// unscoped token makes Slack answer 200 with its HTML login page, so an HTML
// response for a file that did not claim to be HTML is a failed download, not
// a file — attaching it would hand the agent a login page as the user's
// document.
func slackFileContentType(f slackRawFile, responseType string, data []byte) (string, error) {
	declared := strings.ToLower(strings.TrimSpace(f.Mimetype))
	responseType = strings.ToLower(strings.TrimSpace(responseType))
	if responseType == "text/html" && declared != "text/html" {
		return "", errors.New("download returned an HTML page instead of the file (missing files:read scope?)")
	}
	if declared != "" {
		return declared, nil
	}
	if responseType != "" {
		return responseType, nil
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	contentType := http.DetectContentType(sniff)
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return contentType, nil
}

// slackMediaKind maps a mime type onto the normalized media kinds.
func slackMediaKind(contentType string) channel.MsgType {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return channel.MsgTypeImage
	case strings.HasPrefix(contentType, "video/"):
		return channel.MsgTypeVideo
	case strings.HasPrefix(contentType, "audio/"):
		return channel.MsgTypeAudio
	}
	return channel.MsgTypeFile
}

// slackMediaObjectKey derives the object key from the CHAT message the object
// will be attached to, not from the platform message alone: a platform message
// can be ingested twice (the inbound dedup claim is reclaimable once stale),
// and a shared key would run the second ingest into the first one's ledger
// row — possibly a tombstone the intent upsert refuses, silently dropping the
// media.
func slackMediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, f slackRawFile, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", util.UUIDToString(chatMessageID), f.ID, index)))
	return path.Join(
		"workspaces",
		util.UUIDToString(inst.WorkspaceID),
		"slack",
		util.UUIDToString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
}

// slackFileName picks the stored display name: the uploader's own name when it
// is a usable filename, otherwise a generated one keyed by file id + position.
func slackFileName(f slackRawFile, index int, contentType string) string {
	if name := cleanFileName(f.Name); name != "" {
		return name
	}
	return fmt.Sprintf("slack-file-%s-%d%s", safeMediaSegment(f.ID), index+1, mediaExtension(contentType))
}

func cleanFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	// path.Base hands back ".." and "..." unchanged; a name made only of dots
	// is not a filename.
	if strings.Trim(name, ".") == "" || name == "/" {
		return ""
	}
	return name
}

// mediaExtension picks a file extension for a content type, preferring the
// familiar spelling over whatever the mime database happens to list first
// (image/jpeg resolves to ".jfif" on some systems), and pinning the common
// types a slim container image's mime database may not carry.
func mediaExtension(contentType string) string {
	switch contentType {
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
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

// safeMediaSegment reduces an id to characters that are safe in a filename.
func safeMediaSegment(s string) string {
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

func (r *slackMediaResolver) logWarn(msg channel.InboundMessage, err error) {
	r.logger.Warn("slack media resolve skipped", "message_id", msg.MessageID, "error", err)
}

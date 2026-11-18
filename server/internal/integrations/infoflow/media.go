package infoflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inbound media (engine.MediaResolver).
//
// The shape is the one dingtalk/media.go and wecom/media_ingest.go established
// and the Router depends on: HasMedia is a pure in-memory look at the envelope
// already persisted with the message, ResolveMedia runs off the connector ACK
// path after the user message and the dedup mark are durable, every upload is
// covered by an intent-ledger row written BEFORE the object lands, and nothing
// is ever deleted inline — a failure anywhere leaves the row for the reconciler
// and leaves the message's placeholder text intact.
//
// What is infoflow-specific is the fetch: the robot resolves an inbound
// attachment through its own file API (im.File.Download by FID) rather than
// through a URL the platform handed us, so the adapter opens no egress of its
// own on that path. A payload that carries an object key instead of an FID is
// resolved through im.File.GetURL and fetched over the guarded client in
// media_http.go, because a presigned URL is still an untrusted destination.

// Inbound media budgets. The Router's own media deadline is 45 seconds, so the
// whole adapter budget stays deliberately below it: a 30-second fetch timeout
// with two concurrent downloads.
const (
	// maxInboundMediaPerMessage caps one message's attachments. A chat message
	// with more than this is a dump, not a question.
	maxInboundMediaPerMessage = 4
	// maxInboundMediaBytes caps one downloaded object. It is this process's
	// ceiling, not the platform's: every object is buffered in memory to be
	// uploaded, so the cap bounds the heap one message can cost.
	maxInboundMediaBytes = 10 << 20
	// mediaFetchTimeout bounds one attachment's download.
	mediaFetchTimeout = 30 * time.Second
	// mediaFetchConcurrency bounds simultaneous downloads per message.
	mediaFetchConcurrency = 2
	// mediaURLExpirationSeconds is the lifetime asked for when an object key has
	// to be turned into a URL. Only the fetch needs it, so short is right.
	mediaURLExpirationSeconds = 120
)

// mediaStorage is the slice of storage.Storage the resolver drives. ObjectURL
// is a pure function of configuration, which is what lets the intent ledger
// persist the object's URL before the object exists.
type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	ObjectURL(key string) string
}

// mediaPayload is one downloaded attachment: its bytes plus what the platform
// said about them.
type mediaPayload struct {
	Content     []byte
	ContentType string
	Filename    string
	SizeBytes   int64
}

// mediaDownloader turns one descriptor into bytes. It exists as a seam so the
// resolver is a function of its inputs rather than of a live robot, and so a
// test can drive every failure branch without a network.
type mediaDownloader interface {
	Fetch(ctx context.Context, target sendTarget, messageID string, d mediaDescriptor) (mediaPayload, error)
}

// mediaResolver implements engine.MediaResolver for InfoFlow.
type mediaResolver struct {
	// decrypt opens the installation's sealed credentials. The robot that
	// received the message is the only one allowed to read it back, so the
	// resolver rebuilds its fetcher per installation instead of sharing one.
	decrypt Decrypter
	// downloadFor builds the per-installation fetcher from those credentials.
	downloadFor func(credentials) (mediaDownloader, error)
	store       mediaStorage
	ledger      engine.MediaIntentLedger
	logger      *slog.Logger
}

var _ engine.MediaResolver = (*mediaResolver)(nil)

// NewMediaResolver builds the InfoFlow media resolver. store and ledger are
// required — without either there is nothing durable to point an attachment at,
// and the resolver degrades to leaving the placeholder in place. downloadFor is
// defaulted to the SDK fetcher.
func NewMediaResolver(decrypt Decrypter, downloadFor func(credentials) (mediaDownloader, error), store mediaStorage, ledger engine.MediaIntentLedger, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	if downloadFor == nil {
		downloadFor = newMediaDownloaderFor
	}
	if store == nil || ledger == nil {
		logger.Warn("infoflow media: no storage or ledger configured; inbound attachments degrade to their placeholder text")
	}
	return &mediaResolver{decrypt: decrypt, downloadFor: downloadFor, store: store, ledger: ledger, logger: logger}
}

// HasMedia is a pure decode of the envelope persisted with the message. The
// Router calls it synchronously on the ACK path to decide whether the message
// pays for a media deadline, a deferred run and a semaphore slot, so it must do
// no I/O.
func (r *mediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeRawEvent(msg)
	return err == nil && len(raw.Media) > 0
}

// ResolveMedia downloads and stores every attachment on the message, returning
// it with a MediaRef per object that landed. Attachments are independent: one
// that fails does not stop the rest, and the placeholder line for a failed one
// stays in the body — which is exactly what the agent should see.
func (r *mediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeRawEvent(msg)
	if err != nil || len(raw.Media) == 0 {
		return msg
	}
	if len(raw.Media) > maxInboundMediaPerMessage {
		r.logWarn(msg, fmt.Errorf("%d attachments exceed the limit of %d", len(raw.Media), maxInboundMediaPerMessage))
		return msg
	}
	if r.store == nil || r.ledger == nil {
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
	download, err := r.downloadFor(creds)
	if err != nil {
		r.logWarn(msg, fmt.Errorf("build media fetcher: %w", err))
		return msg
	}
	target := targetFromChatID(raw.ChatID)

	refs := make([]channel.MediaRef, len(raw.Media))
	valid := make([]bool, len(raw.Media))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(mediaFetchConcurrency)
	for i, d := range raw.Media {
		g.Go(func() error {
			key := mediaObjectKey(inst, chatMessageID, raw.MessageID, i, d.Kind)
			link := r.store.ObjectURL(key)
			// The ledger row goes first: from here on every failure — download,
			// upload, a crash — leaves an intent the reconciler settles, and
			// nothing below deletes anything.
			owned, err := r.ledger.RecordPendingMediaObject(gctx, engine.RecordPendingMediaObjectParams{
				StorageKey:     key,
				WorkspaceID:    inst.WorkspaceID,
				ChatMessageID:  chatMessageID,
				StorageURL:     link,
				InstallationID: inst.ID,
			})
			if err != nil {
				r.logWarn(msg, fmt.Errorf("record media intent: %w", err))
				return nil
			}
			if !owned {
				// The reconciler owns this key; never resurrect it.
				r.logWarn(msg, fmt.Errorf("media key %s is owned by the reconciler", key))
				return nil
			}
			got, err := download.Fetch(gctx, target, raw.MessageID, d)
			if err != nil {
				r.logWarn(msg, fmt.Errorf("fetch attachment %d: %w", i+1, err))
				return nil
			}
			if int64(len(got.Content)) > maxInboundMediaBytes {
				r.logWarn(msg, fmt.Errorf("attachment %d is %d bytes, over the %d-byte cap", i+1, len(got.Content), maxInboundMediaBytes))
				return nil
			}
			filename := inboundMediaName(d, got, i)
			contentType := inboundMediaContentType(d, got, filename)
			if _, err := r.store.Upload(gctx, key, got.Content, contentType, filename); err != nil {
				r.logWarn(msg, fmt.Errorf("upload attachment %d: %w", i+1, err))
				return nil
			}
			refs[i] = channel.MediaRef{
				Type:       d.Kind,
				StorageKey: key,
				StorageURL: link,
				Filename:   filename,
				MimeType:   contentType,
				SizeBytes:  int64(len(got.Content)),
			}
			valid[i] = true
			return nil
		})
	}
	_ = g.Wait()
	for i, ref := range refs {
		if valid[i] {
			msg.MediaRefs = append(msg.MediaRefs, ref)
		}
	}
	return msg
}

func (r *mediaResolver) logWarn(msg channel.InboundMessage, err error) {
	r.logger.Warn("infoflow media ingest failed",
		"message_id", msg.MessageID, "chat_id", msg.Source.ChatID, "error", err)
}

// mediaObjectKey names the object. It is derived from the CHAT message rather
// than from the platform message for the reason lark and wecom document: one
// platform message can be ingested twice (the inbound dedup claim is reclaimable
// once stale), and a shared key would run the second ingest into the first one's
// ledger row — possibly a tombstone the intent upsert refuses, silently dropping
// the media. The attachment's position in the message is part of the key too,
// since a mixed message can carry several and the platform reference is not
// stable enough to key on.
func mediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int, kind channel.MsgType) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d",
		util.UUIDToString(chatMessageID), messageID, string(kind), index)))
	return path.Join(
		"workspaces",
		util.UUIDToString(inst.WorkspaceID),
		"infoflow",
		util.UUIDToString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
}

// inboundMediaName works out what to call the stored object. The name is shown
// to the agent and written into the attachment row, so it is a bare file name:
// never a platform-supplied path, and never empty.
func inboundMediaName(d mediaDescriptor, got mediaPayload, index int) string {
	name := strings.TrimSpace(firstNonEmpty(got.Filename, d.Filename))
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" || strings.ContainsAny(name, "\r\n\x00") {
		name = ""
	}
	if len(name) > 120 {
		name = name[:120]
	}
	if name != "" {
		return name
	}
	return fmt.Sprintf("infoflow-%s-%d%s", d.Kind, index+1, mediaExtension(d, got))
}

// inboundMediaContentType settles the stored object's media type. The platform
// header wins when it is usable; otherwise the file name's extension decides;
// otherwise it is a generic download, which is what a chat file with no
// metadata actually is.
func inboundMediaContentType(d mediaDescriptor, got mediaPayload, filename string) string {
	if ct := baseContentType(got.ContentType); ct != "" {
		return ct
	}
	if ct := baseContentType(d.MimeType); ct != "" {
		return ct
	}
	if ext := path.Ext(filename); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	return "application/octet-stream"
}

// mediaExtension picks a file extension from the content type or the platform's
// own file name, for the case where neither gave us a usable name.
func mediaExtension(d mediaDescriptor, got mediaPayload) string {
	if ext := path.Ext(strings.TrimSpace(firstNonEmpty(got.Filename, d.Filename))); len(ext) > 1 && len(ext) <= 10 {
		return strings.ToLower(ext)
	}
	ct := baseContentType(firstNonEmpty(got.ContentType, d.MimeType))
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}

// newMediaDownloaderFor builds the production fetcher from decrypted credentials.
func newMediaDownloaderFor(creds credentials) (mediaDownloader, error) {
	client, err := newSDKClient(creds)
	if err != nil {
		return nil, err
	}
	return sdkMediaDownloader{client: client, http: newMediaHTTPClient()}, nil
}

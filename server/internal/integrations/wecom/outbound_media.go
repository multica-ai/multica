package wecom

// outbound_media.go — delivering the files an agent produced.
//
// The agent's side of this already exists and is platform-agnostic: it runs
// `multica attachment upload <path>`, the file lands in object storage, and
// CompleteTask binds the row to the assistant message it just wrote. What was
// missing was the last hop. Everything downstream of that bind assumed a chat
// window in a browser, so a WeCom conversation was told it could not take
// files at all.
//
// Three things decide the shape here.
//
// The answer goes first, always. An upload is megabytes and round trips and it
// can fail; the sentence the agent wrote cannot be made to wait behind one, and
// must not be lost to one. So this runs after the reply is out, on its own
// goroutine and its own budget, and its worst outcome is one extra line saying
// a file did not make it.
//
// The file is its own message. The long connection has no msg_item, so nothing
// can be embedded in a reply — "answer with an attachment" is necessarily two
// messages.
//
// And WeCom validates bytes against the msgtype. A .pptx declared as an image
// is refused rather than converted, and each kind has its own size ceiling, so
// what to call a file is a decision and not a lookup.

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mediaObjectStore is the slice of storage.Storage this path needs: the
// attachment row carries the object's URL, and these two turn it back into
// bytes.
type mediaObjectStore interface {
	KeyFromURL(rawURL string) string
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

// mediaSendFailedText is what the user is told when a file did not make it.
// Hardcoded Chinese, like every other user-facing string this adapter sends
// (replier.go) — WeCom deployments are China-only.
const mediaSendFailedText = "⚠️ 有文件没能发出来，我这边保留着，需要的话我再试一次。"

// attachmentBudget bounds one answer's whole attachment delivery — reading
// every object, uploading it, and sending it. Generous because a 50MB file over
// a hundred acked chunks is not fast, and nothing is waiting on it.
const attachmentBudget = 5 * time.Minute

// The per-kind ceilings WeCom applies to uploaded material: 10MB for a photo,
// 10MB for a video, 2MB for a voice note. Bytes past a kind's ceiling still
// travel — as a file, which has the widest limit of the four — because a file
// card the user can open beats a photo the server refused.
const (
	maxOutboundImageBytes = 10 << 20
	maxOutboundVoiceBytes = 2 << 20
	maxOutboundVideoBytes = 10 << 20
)

// attachmentTarget is where one answer's files are going: the installation
// whose socket carries them, and the conversation at the other end.
type attachmentTarget struct {
	InstallationID pgtype.UUID
	ChatID         string
	ChatType       int
}

// OutboundOption configures the chat-done subscriber at construction.
type OutboundOption func(*Outbound)

// WithAttachments turns on file delivery. Without it — a deployment with no
// object storage — an answer is delivered exactly as it was before, and the
// agent is told as much in its brief.
func WithAttachments(objects mediaObjectStore) OutboundOption {
	return func(o *Outbound) { o.objects = objects }
}

// mayCarryAttachments reports whether this turn is worth the lookups even
// though the agent said nothing. Everything it checks is already in hand, so a
// deployment with no storage — or an event naming no message — costs no query.
func (o *Outbound) mayCarryAttachments(e events.Event) bool {
	return o.objects != nil && e.WorkspaceID != "" && chatDoneMessageID(e.Payload) != ""
}

// deliverAttachments hands the answer's files to a goroutine of their own, if
// there are any to hand over. It is called after the words are out, and
// returns immediately.
func (o *Outbound) deliverAttachments(e events.Event, to attachmentTarget) {
	if o.objects == nil || o.senders == nil {
		return
	}
	messageID, err := util.ParseUUID(chatDoneMessageID(e.Payload))
	if err != nil || !messageID.Valid {
		return // a turn with no assistant message has nothing bound to it
	}
	workspaceID, err := util.ParseUUID(e.WorkspaceID)
	if err != nil || !workspaceID.Valid {
		return
	}
	if !to.InstallationID.Valid || to.ChatID == "" {
		return
	}
	// Shed before spawning when too many deliveries are already outstanding.
	// The semaphore below bounds how many RUN at once; without this,
	// goroutines still accumulate without limit while they wait for it, and a
	// workspace whose agents emit artifacts steadily would grow them forever.
	if !o.claimAttachmentSlot() {
		o.logger.Warn("wecom outbound: attachment delivery shed, too many already pending",
			"installation_id", uuidStringPub(to.InstallationID),
			"pending", maxPendingAttachmentDeliveries)
		return
	}
	o.spawn(func() {
		defer o.releaseAttachmentSlot()

		ctx, cancel := context.WithTimeout(context.Background(), attachmentBudget)
		defer cancel()

		// Acquire INSIDE the goroutine, never before the spawn. Bus.Publish is
		// synchronous on the task-completion goroutine, so blocking out there
		// would wedge the completion path for up to the attachment budget —
		// which is the very thing the spawn exists to prevent.
		select {
		case attachmentSlots <- struct{}{}:
			defer func() { <-attachmentSlots }()
		case <-ctx.Done():
			o.logger.Warn("wecom outbound: attachment delivery gave up waiting for a slot",
				"installation_id", uuidStringPub(to.InstallationID))
			return
		}
		o.sendAttachments(ctx, messageID, workspaceID, to)
	})
}

// sendAttachments delivers every file bound to one answer. Files are
// independent: one that fails does not stop the rest, and whatever did not make
// it is said once at the end rather than once each.
func (o *Outbound) sendAttachments(ctx context.Context, messageID, workspaceID pgtype.UUID, to attachmentTarget) {
	rows, err := o.q.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID,
		WorkspaceID:   workspaceID,
	})
	if err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: attachment lookup failed",
			"error", err, "chat_message_id", uuidStringPub(messageID))
		return
	}
	if len(rows) == 0 {
		return
	}
	// Resolved here rather than carried in from the caller: the send that
	// delivered the words may have been minutes ago on a socket since replaced,
	// and the registry always holds the live one.
	sender := o.senders.get(to.InstallationID)
	if sender == nil {
		o.logger.WarnContext(ctx, "wecom outbound: no live connection for attachment delivery",
			"installation_id", uuidStringPub(to.InstallationID), "attachments", len(rows))
		return
	}
	failed := 0
	for _, row := range rows {
		if err := o.sendAttachment(ctx, sender, row, to); err != nil {
			failed++
			// The object's URL stays out of the log: it is an address that
			// serves the file to whoever holds it.
			o.logger.WarnContext(ctx, "wecom outbound: attachment not delivered",
				"error", err,
				"installation_id", uuidStringPub(to.InstallationID),
				"attachment_id", uuidStringPub(row.ID),
				"content_type", row.ContentType,
				"size_bytes", row.SizeBytes)
		}
	}
	if failed == 0 {
		return
	}
	// The answer is already on the user's screen and it may well refer to a
	// file. Saying nothing would leave them looking for one that never comes.
	if err := sender.sendTextCtx(ctx, to.ChatID, to.ChatType, mediaSendFailedText); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: could not say the file failed",
			"error", err, "installation_id", uuidStringPub(to.InstallationID))
	}
}

// sendAttachment carries one file from object storage into the chat.
func (o *Outbound) sendAttachment(ctx context.Context, sender *wsSender, row db.Attachment, to attachmentTarget) error {
	data, err := o.readObject(ctx, row.Url)
	if err != nil {
		return err
	}
	kind := wecomMediaKind(row.ContentType, row.Filename, len(data))
	name := outboundMediaName(row.Filename, row.ContentType)

	mediaID, err := sender.uploadMedia(ctx, outboundMedia{
		Kind:     kind,
		Filename: name,
		Data:     data,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", kind, err)
	}
	// Video is the only kind with fields beyond the media_id, and both are
	// required. The file's own name is what there is to say about it — the
	// attachment row carries no caption and the agent's words are already in
	// the message above.
	return sender.sendMedia(ctx, to.ChatID, to.ChatType, mediaSend{
		Kind:        kind,
		MediaID:     mediaID,
		Title:       strings.TrimSuffix(name, path.Ext(name)),
		Description: name,
	})
}

// readObject pulls the whole file into memory. It has to be whole: the upload
// declares total_size and total_chunks before the first chunk goes out, so
// there is no streaming this one.
func (o *Outbound) readObject(ctx context.Context, rawURL string) ([]byte, error) {
	key := o.objects.KeyFromURL(rawURL)
	if key == "" {
		return nil, fmt.Errorf("wecom: attachment is not an object this deployment stores")
	}
	rc, err := o.objects.GetReader(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	defer rc.Close()
	// One byte of headroom, so reading exactly the cap can be told from a file
	// that has more to come.
	data, err := io.ReadAll(io.LimitReader(rc, maxMediaUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if len(data) > maxMediaUploadBytes {
		return nil, errMediaUploadTooLarge
	}
	return data, nil
}

// wecomMediaKind decides what WeCom is told this file is.
//
// The content type leads, because it is what the uploader declared. When it
// says nothing useful — empty, or the octet-stream that means "bytes" — the
// filename's extension is the better guess. A kind whose ceiling the file
// exceeds is demoted to a file rather than sent and refused.
func wecomMediaKind(contentType, filename string, size int) mediaMsgType {
	ct := baseContentType(contentType)
	if ct == "" || ct == "application/octet-stream" {
		ct = baseContentType(mime.TypeByExtension(path.Ext(filename)))
	}
	switch {
	case strings.HasPrefix(ct, "image/") && size <= maxOutboundImageBytes:
		return mediaTypeImage
	case strings.HasPrefix(ct, "video/") && size <= maxOutboundVideoBytes:
		return mediaTypeVideo
	// Voice is AMR only. An mp3 sent as a voice note is refused, and as a file
	// it is at least playable after a tap.
	case ct == "audio/amr" && size <= maxOutboundVoiceBytes:
		return mediaTypeVoice
	default:
		return mediaTypeFile
	}
}

// baseContentType drops the parameters a content type may carry, so
// "text/csv; charset=utf-8" compares as "text/csv".
func baseContentType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if semi := strings.IndexByte(s, ';'); semi >= 0 {
		s = strings.TrimSpace(s[:semi])
	}
	return s
}

// outboundMediaName is what the recipient sees on the file card. It is reduced
// to a single path segment — the name reaches the wire and a stored filename is
// not guaranteed to be one — and given an extension when it has none, since
// that is the only hint WeCom gets about the format.
func outboundMediaName(filename, contentType string) string {
	name := cleanMediaFilename(filename)
	if name == "" {
		name = "attachment"
	}
	if path.Ext(name) == "" {
		if ext := mediaExtension(baseContentType(contentType)); ext != "" {
			name += ext
		}
	}
	return name
}

// cleanMediaFilename reduces a stored filename to one path segment. A stored
// name is not guaranteed to be one — it came from an uploader — and this one
// reaches the wire.
func cleanMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

// mediaExtension picks the extension for a content type. The listed few are
// pinned because mime's own answer for them varies with the host's mime.types
// — "image/jpeg" can come back as ".jfif", which is a name no recipient
// recognizes even though the bytes are fine.
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
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

// chatDoneMessageID pulls the assistant message id out of a chat:done payload
// (the typed payload, or its map form after a serialization round trip). It is
// the key every attachment on this turn is bound to.
func chatDoneMessageID(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.MessageID
	case map[string]any:
		if s, ok := p["message_id"].(string); ok {
			return s
		}
	}
	return ""
}

// attachmentSlots caps how many attachment deliveries read an object at once.
//
// Process-wide, not per installation: the heap is process-wide, and a
// per-installation cap on a deployment running several bots just multiplies.
// Each delivery holds one object while it chunks it up the socket, so this is
// the number that decides peak resident attachment bytes.
var attachmentSlots = make(chan struct{}, maxConcurrentAttachmentDeliveries)

const (
	// maxConcurrentAttachmentDeliveries is how many objects may be in flight.
	// Small on purpose: each one is up to the platform's file ceiling, and
	// the socket they share is a single long connection per bot, so more
	// concurrency buys queueing rather than throughput.
	maxConcurrentAttachmentDeliveries = 2

	// maxPendingAttachmentDeliveries bounds the goroutines waiting for a
	// slot. Past it a delivery is shed with a log rather than queued: the
	// answer's text has already reached the user, the attachment is still in
	// object storage, and an unbounded queue of goroutines each holding a
	// completion's context is the failure this exists to avoid.
	maxPendingAttachmentDeliveries = 32
)

// claimAttachmentSlot reserves one of the pending slots, or reports that the
// backlog is full.
func (o *Outbound) claimAttachmentSlot() bool {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	if o.pendingAttachments >= maxPendingAttachmentDeliveries {
		return false
	}
	o.pendingAttachments++
	return true
}

func (o *Outbound) releaseAttachmentSlot() {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	o.pendingAttachments--
}

package lark

// outbound_media.go — the last hop: getting an agent's files into the chat.
//
// The agent's side of this already exists and is platform-agnostic. It runs
// `multica attachment upload <path>`, the file lands in object storage, and
// task completion binds the row to the assistant chat_message it just wrote
// (BindChatAttachmentsToMessage). Everything downstream of that bind assumed
// a chat window in a browser, so a Feishu conversation was told it could not
// take files at all. This file is the missing hop.
//
// Three things decide the shape, and two of them are the same decisions the
// WeCom adapter made (integrations/wecom/outbound_media.go) because they
// follow from the medium rather than from the vendor.
//
// The answer goes first, always. An upload is megabytes and round trips and
// it can fail; the sentence the agent wrote cannot be made to wait behind
// one, and must not be lost to one. So this runs AFTER the reply is out, on
// its own goroutine and its own budget, and its worst outcome is one extra
// line saying a file did not make it.
//
// Each file is its own message. Lark CAN embed an image in an interactive
// card, and so could have folded a set of images into one bubble — but the
// answer has already gone out by the time these bytes exist, so the images
// are necessarily a separate message from the prose either way, and
// `msg_type=image` is both the simpler envelope and the one Lark renders
// full-width. Non-images have no embedding option at all: `msg_type=file`
// is the only wire shape.
//
// The third decision is Lark's, not ours: it validates an upload against the
// declared file_type and refuses a mismatch rather than downgrading it, so
// what to call a file is a choice with a wrong answer and not a lookup. See
// larkFileTypeFor.
//
// What this path deliberately does NOT do is tell the user a file was sent
// when the send was not confirmed. Every exit below ends in either a
// delivery or a sentence; there is no exit that ends in silence while a
// file was known to be waiting.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mediaAPIClient is the slice of the Lark transport this path needs.
//
// Deliberately separate from APIClient rather than four more methods on it
// — see the header of http_client_media.go for why. A client that does not
// implement it (the production stub, the test fakes) simply never delivers
// attachments, which is the same degradation as a deployment with no
// storage configured, and the same one that holds today.
type mediaAPIClient interface {
	UploadImage(ctx context.Context, p UploadImageParams) (string, error)
	UploadFile(ctx context.Context, p UploadFileParams) (string, error)
	SendImageMessage(ctx context.Context, p SendImageParams) (string, error)
	SendFileMessage(ctx context.Context, p SendFileParams) (string, error)
}

// mediaObjectStore is the slice of storage.Storage this path needs: the
// attachment row carries the object's URL, and these two turn it back into
// bytes. Same shape the WeCom adapter declares, for the same reason — the
// alternate implementation (LocalStorage in self-hosted deployments) only
// has to satisfy exactly what is used.
type mediaObjectStore interface {
	KeyFromURL(rawURL string) string
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

// What the user is told when a file does not arrive. English, matching every
// other user-facing string this adapter sends (replier.go, outbound.go) — and
// unlike the WeCom sibling, whose strings are hardcoded Chinese because WeCom
// deployments are China-only, one Lark adapter serves both the mainland
// Feishu cloud and the international Lark cloud (see channel.go).
const (
	// mediaSendFailedText — we know a file did not make it. Definite,
	// because claiming a definite failure that later turns out to be a
	// delivery is how a user learns to ignore the notice.
	mediaSendFailedText = "⚠️ A file didn't make it. I still have it — say the word and I'll resend."

	// mediaLookupFailedText — the failure is on our side and before the
	// question was even answered: we could not read what was attached to
	// this reply, so we do not know whether there was a file. Saying
	// nothing here is what leaves a user waiting for something that was
	// never attempted.
	mediaLookupFailedText = "⚠️ I couldn't check whether this reply had a file attached. If it did, it wasn't sent — say the word and I'll resend."
)

// attachmentBudget bounds one answer's whole attachment delivery — reading
// every object, uploading it, and sending it. Generous because nothing is
// waiting on it: the reply is already in the chat, and the worst this
// timeout costs is a notice that a file was skipped.
const attachmentBudget = 5 * time.Minute

// maxConcurrentAttachmentDeliveries is how many answers may be reading and
// uploading objects at once.
//
// Process-wide rather than per installation: the heap is process-wide, so a
// per-installation cap on a deployment running several bots just multiplies.
// Each delivery may hold a whole image in memory at once, so this is the
// number that decides peak resident attachment bytes.
const maxConcurrentAttachmentDeliveries = 3

var attachmentSlots = make(chan struct{}, maxConcurrentAttachmentDeliveries)

// SetAttachments wires the object store this deployment's attachments live
// in. Mirroring SetTypingIndicatorManager: the dependency is optional, and
// leaving it unset is the supported "no storage here" configuration rather
// than a programming error.
//
// Passing a store is also the promise that agents will be told they can
// attach files (see router.go) — a run must not be promised a delivery hop
// that was never built.
func (p *Patcher) SetAttachments(objects mediaObjectStore) {
	p.objects = objects
}

// chatDoneMessageID pulls the assistant message id out of a chat:done
// payload (the typed payload, or its map form after a serialization round
// trip). It is the key every attachment on this turn is bound to, and its
// absence means there is nothing to look up.
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

// deliverAttachmentsAsync starts the second hop for one answer.
//
// Called after the reply has been sent, and returns immediately: the caller
// is the bus's synchronous publish goroutine on the task-completion path, so
// blocking there for an upload would wedge task completion behind it. That
// is the same reason this is a spawn rather than a call — see the file
// header for the first half of the argument.
func (p *Patcher) deliverAttachmentsAsync(creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, workspaceID pgtype.UUID, payload any) {
	if p.objects == nil || p.media == nil {
		return
	}
	messageID := chatDoneMessageID(payload)
	if messageID == "" {
		// No assistant message, so nothing was bound to one.
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), attachmentBudget)
		defer cancel()
		p.deliverAttachments(ctx, creds, binding, target, workspaceID, messageID)
	}()
}

// deliverAttachments reads every file bound to one answer and sends it.
//
// Files are independent: one that fails does not stop the rest, and what is
// known about the ones that plainly did not arrive is said once at the end
// rather than once each. The user gets at most one extra line per answer.
func (p *Patcher) deliverAttachments(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, workspaceID pgtype.UUID, rawMessageID string) {
	messageID, err := util.ParseUUID(rawMessageID)
	if err != nil || !messageID.Valid {
		// A malformed id can only mean a producer bug; there is no user
		// action it could be reporting, so it is a log line only.
		p.cfg.Logger.Warn("lark outbound: chat:done carried an unparseable message id",
			"message_id", rawMessageID, "error", err)
		return
	}
	rows, err := p.queries.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID,
		WorkspaceID:   workspaceID,
	})
	if err != nil {
		p.cfg.Logger.Warn("lark outbound: attachment lookup failed",
			"error", err, "chat_message_id", rawMessageID)
		p.tellUser(ctx, creds, binding, target, mediaLookupFailedText)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Past here a file is known to be waiting, so every way out of this
	// function has to end in either a delivery or a sentence to the user.
	select {
	case attachmentSlots <- struct{}{}:
		defer func() { <-attachmentSlots }()
	default:
		// Shed rather than queue: an unbounded backlog of image reads is
		// heap, and the answer is already in the chat. Counted so the
		// notice is not a surprise.
		p.cfg.Logger.Warn("lark outbound: attachment delivery shed, too many already pending",
			"chat_message_id", rawMessageID, "attachments", len(rows))
		p.tellUser(ctx, creds, binding, target, mediaSendFailedText)
		return
	}

	chatID := outboundChatID(binding)
	failed := 0
	for _, row := range rows {
		if err := p.deliverOneAttachment(ctx, creds, chatID, target, row); err != nil {
			failed++
			p.cfg.Logger.Warn("lark outbound: attachment delivery failed",
				"error", err,
				"attachment_id", uuidString(row.ID),
				"filename", row.Filename,
				"chat_message_id", rawMessageID)
		}
	}
	if failed > 0 {
		p.tellUser(ctx, creds, binding, target, mediaSendFailedText)
	}
}

// deliverOneAttachment reads one attachment out of object storage, uploads
// it to Lark, and posts it.
//
// Images and everything else split here rather than at the lookup because
// only the bytes decide which endpoint they can go to, and the classification
// is the attachment row's own content type — not the extension, which the
// uploader may not have controlled.
func (p *Patcher) deliverOneAttachment(ctx context.Context, creds InstallationCredentials, chatID ChatID, target ReplyTarget, row db.Attachment) error {
	rawURL := strings.TrimSpace(row.Url)
	if rawURL == "" {
		return errors.New("attachment has no url")
	}
	key := p.objects.KeyFromURL(rawURL)
	if key == "" {
		return fmt.Errorf("object key not derivable from url %q", rawURL)
	}
	rc, err := p.objects.GetReader(ctx, key)
	if err != nil {
		return fmt.Errorf("read object %q: %w", key, err)
	}
	defer rc.Close()
	// Read the whole object: both Lark endpoints take a complete
	// multipart body, and both ceilings are small enough (10 / 30 MiB)
	// that holding one is the point of the concurrency cap above. Read
	// one byte past the larger ceiling so "too big" is decided here
	// rather than by a truncated upload.
	data, err := io.ReadAll(io.LimitReader(rc, larkFileUploadMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read object %q: %w", key, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("object %q is empty", key)
	}

	if isImageAttachment(row.ContentType, row.Filename) {
		if len(data) > larkImageUploadMaxBytes {
			return fmt.Errorf("image %q is %d bytes, over the Lark image limit", row.Filename, len(data))
		}
		imageKey, err := p.media.UploadImage(ctx, UploadImageParams{
			InstallationID: creds,
			Filename:       row.Filename,
			Data:           data,
		})
		if err != nil {
			return fmt.Errorf("upload image: %w", err)
		}
		if _, err := p.media.SendImageMessage(ctx, SendImageParams{
			InstallationID: creds,
			ChatID:         chatID,
			ImageKey:       imageKey,
			ReplyTarget:    target,
		}); err != nil {
			return fmt.Errorf("send image: %w", err)
		}
		return nil
	}

	if len(data) > larkFileUploadMaxBytes {
		return fmt.Errorf("file %q is %d bytes, over the Lark file limit", row.Filename, len(data))
	}
	fileKey, err := p.media.UploadFile(ctx, UploadFileParams{
		InstallationID: creds,
		Filename:       row.Filename,
		FileType:       larkFileTypeFor(row.Filename),
		Data:           data,
	})
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	if _, err := p.media.SendFileMessage(ctx, SendFileParams{
		InstallationID: creds,
		ChatID:         chatID,
		FileKey:        fileKey,
		ReplyTarget:    target,
	}); err != nil {
		return fmt.Errorf("send file: %w", err)
	}
	return nil
}

// tellUser posts a short plain-text notice into the same thread as the
// answer. Best-effort: a failed notice is a log line, because there is
// nothing left to tell the user with.
func (p *Patcher) tellUser(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, body string) {
	// The notice goes through the ordinary text sender, not the media
	// client: it is the one send that must work even when the media path
	// is the thing that failed.
	if _, err := p.client.SendTextMessage(ctx, SendTextParams{
		InstallationID: creds,
		ChatID:         outboundChatID(binding),
		Text:           prependTextMention(mentionOpenID(binding), body),
		ReplyTarget:    target,
	}); err != nil {
		p.cfg.Logger.Warn("lark outbound: failed to tell the user about an attachment problem",
			"error", err, "notice", body)
	}
}

// isImageAttachment decides whether a row goes to the image endpoint.
//
// Content type first and extension second, deliberately: the content type is
// what the uploader recorded and what Lark's renderer keys off, while the
// extension is user-supplied text. A row carrying neither falls to the file
// path, which accepts arbitrary bytes — the failure mode of guessing "image"
// wrongly is a refused upload, and of guessing "file" wrongly is only a
// less-nice bubble.
func isImageAttachment(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if ct != "" {
		return strings.HasPrefix(ct, "image/")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

package telegram

// Outbound media: the last hop for files an agent produced. The agent side is
// platform-agnostic — `multica attachment upload <path>` puts the file in
// object storage and CompleteTask binds it to the assistant message. This file
// sends those attachments into the Telegram chat once the reply text has
// settled, each as its own sendPhoto / sendDocument / … message (Telegram has
// no way to embed a file in a text message). Mirrors wecom/outbound_media.go.
//
// Duplicates: delivery starts only from the call that ended the turn — settled
// it as delivered, or closed it as an empty completion — and the delivery row
// lets exactly one call do that across replicas, replays and retries; a
// duplicate chat:done finds the turn already ended and sends nothing. A crash
// mid-way loses the remaining files; nothing resends, because Telegram offers
// no idempotency key and a duplicate cannot be taken back.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Bot API upload ceilings: 10 MB for a photo, 50 MB for anything else. A photo
// past its ceiling still travels, as a document.
const (
	maxOutboundPhotoBytes = 10 << 20
	maxOutboundFileBytes  = 50 << 20
	// attachmentBudget bounds one reply's whole delivery — every object read
	// and every upload. Nothing waits on it.
	attachmentBudget = 5 * time.Minute
	// maxConcurrentAttachmentDeliveries bounds the deliveries in flight, and
	// with them the goroutines and the resident bytes (one object at a time
	// each). Admission is decided before anything is spawned: a reply that
	// finds every slot busy is shed with the notice, never queued.
	maxConcurrentAttachmentDeliveries = 4
)

// attachmentNoticeText is said once per reply when a file may not have
// arrived. It has to hold in every ending that leads here — a refused upload,
// a shed delivery, and a send whose response was lost after Telegram accepted
// it — so it claims nothing about what did or did not land.
const attachmentNoticeText = "⚠️ I couldn't confirm that every file from this reply reached Telegram. Anything missing is still attached to the reply in Multica."

// EnableFileDelivery turns on the attachment hop. Call at boot, before
// Register. Without it — no object storage — replies are text only, and the
// agent is told as much per turn.
func (o *Outbound) EnableFileDelivery(objects objectStore) {
	o.objects = objects
}

var attachmentSlots = make(chan struct{}, maxConcurrentAttachmentDeliveries)

// deliverAttachments sends the files bound to a reply once its text is in the
// chat. The lookup runs here, on the terminal worker: a reply with nothing
// bound — the common case — costs one indexed read and no goroutine, and a
// delivery is spawned only for files known to exist, under a slot claimed
// first. Called after the turn is ended, so the lease is no longer held.
func (o *Outbound) deliverAttachments(ctx context.Context, reply *terminalReply) {
	if o.objects == nil || reply.target == nil {
		return
	}
	messageID, err := util.ParseUUID(chatDoneMessageID(reply.event.Payload))
	if err != nil || !messageID.Valid {
		return // a turn with no assistant message has nothing bound to it
	}
	workspaceID, err := util.ParseUUID(reply.event.WorkspaceID)
	if err != nil || !workspaceID.Valid {
		return
	}
	// Detached from the call budget the text delivery ran under, which may
	// have just about run out; the read is short and its own.
	ctx, cancel := o.recordContext(ctx)
	defer cancel()
	rows, err := o.q.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID, WorkspaceID: workspaceID,
	})
	if err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: attachment lookup failed", "error", err, "chat_message_id", uuidText(messageID))
		return
	}
	if len(rows) == 0 {
		return
	}
	target := *reply.target
	select {
	case attachmentSlots <- struct{}{}:
	default:
		// Every slot is uploading. The files are known to exist, so this is
		// said, not just logged; they stay in Multica.
		o.logger.WarnContext(ctx, "telegram outbound: attachment delivery shed, every slot busy",
			"attachments", len(rows), "chat_message_id", uuidText(messageID))
		o.tellUser(ctx, newBotAPI(o.apiBase, target.botToken, o.client), target)
		return
	}
	o.spawn(func() {
		defer func() { <-attachmentSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), attachmentBudget)
		defer cancel()
		o.sendAttachments(ctx, rows, target)
	})
}

// sendAttachments delivers every file bound to one reply. Files are
// independent: one that fails does not stop the rest.
func (o *Outbound) sendAttachments(ctx context.Context, rows []db.Attachment, target replyTarget) {
	api := newBotAPI(o.apiBase, target.botToken, o.client)
	failed := 0
	for _, row := range rows {
		if err := o.sendAttachment(ctx, api, row, target); err != nil {
			failed++
			o.logger.WarnContext(ctx, "telegram outbound: attachment not confirmed delivered",
				"error", err, "attachment_id", uuidText(row.ID), "content_type", row.ContentType, "size_bytes", row.SizeBytes)
		}
	}
	if failed > 0 {
		o.tellUser(ctx, api, target)
	}
}

// tellUser puts the notice into the conversation, best effort: every caller
// is already on a path where something went wrong.
func (o *Outbound) tellUser(ctx context.Context, api *botAPI, target replyTarget) {
	if _, err := api.SendMessage(ctx, sendMessageParams{
		ChatID: target.chatID, Text: attachmentNoticeText, MessageThreadID: target.threadID,
	}); err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: could not tell the user about the file", "error", err)
	}
}

// sendAttachment carries one file from object storage into the chat. A photo
// Telegram refuses to process (a 400 — bad dimensions, an SVG, a PNG it will
// not convert) is sent again as a document: a 400 means nothing was posted,
// and a file card beats a dropped image. Any other error is final, and not a
// verdict: a lost response may mean the message already landed, which is why
// nothing here retries and why the notice is worded the way it is.
func (o *Outbound) sendAttachment(ctx context.Context, api *botAPI, row db.Attachment, target replyTarget) error {
	if row.SizeBytes > maxOutboundFileBytes {
		return fmt.Errorf("attachment is %d bytes, over the %d MiB upload limit", row.SizeBytes, maxOutboundFileBytes>>20)
	}
	data, err := o.readObject(ctx, row.Url)
	if err != nil {
		return err
	}
	params := sendMediaParams{
		ChatID: target.chatID, MessageThreadID: target.threadID,
		Field: outboundMediaField(row.ContentType, len(data)), Filename: outboundMediaName(row.Filename, row.ContentType),
		ContentType: row.ContentType, Data: data,
	}
	_, err = api.SendMedia(ctx, params)
	if err != nil && params.Field != "document" && isBadRequest(err) {
		params.Field = "document"
		_, err = api.SendMedia(ctx, params)
	}
	return err
}

func (o *Outbound) readObject(ctx context.Context, rawURL string) ([]byte, error) {
	key := o.objects.KeyFromURL(rawURL)
	if key == "" {
		return nil, errors.New("attachment is not an object this deployment stores")
	}
	rc, err := o.objects.GetReader(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxOutboundFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if len(data) > maxOutboundFileBytes {
		return nil, fmt.Errorf("attachment is over the %d MiB upload limit", maxOutboundFileBytes>>20)
	}
	return data, nil
}

// outboundMediaField picks the Bot API method by content type: photos render
// inline, mp4 video and common audio get native players, everything else is a
// file card. Telegram's converters only handle the common formats, so the
// fallback in sendAttachment covers the rest.
func outboundMediaField(contentType string, size int) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if semi := strings.IndexByte(ct, ';'); semi >= 0 {
		ct = strings.TrimSpace(ct[:semi])
	}
	switch {
	case (ct == "image/jpeg" || ct == "image/png" || ct == "image/gif" || ct == "image/webp") && size <= maxOutboundPhotoBytes:
		return "photo"
	case ct == "video/mp4":
		return "video"
	case ct == "audio/mpeg" || ct == "audio/mp4" || ct == "audio/ogg":
		return "audio"
	default:
		return "document"
	}
}

// outboundMediaName is what the recipient sees on the file card: one path
// segment, with an extension when the stored name has none.
func outboundMediaName(filename, contentType string) string {
	name := cleanFilename(filename)
	if name == "" {
		name = "attachment"
	}
	if path.Ext(name) == "" {
		name += mediaExtension(strings.ToLower(strings.TrimSpace(contentType)))
	}
	return name
}

func isBadRequest(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == http.StatusBadRequest
}

// chatDoneMessageID pulls the assistant message id out of a chat:done payload
// (typed, or its map form after a serialization round trip).
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

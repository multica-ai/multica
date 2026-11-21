package infoflow

// outbound_media.go — delivering the files an agent produced.
//
// The agent's side of this is already platform-agnostic: it runs
// `multica attachment upload <path>`, the file lands in object storage, and
// task completion binds the row to the assistant message it just wrote. What
// InfoFlow needs is the last hop, and the platform decides its shape.
//
// InfoFlow's bot API splits along exactly the line this path runs on:
//
//   - An image is a first-class message. `SendUserMessage{type:"image"}` and
//     the group `IMAGE` body both take the **BOS object key** returned by
//     `File.Upload` as their content, so a picture is delivered as a picture.
//   - A file is not. The SDK refuses single-chat `file` sends outright
//     ("单聊 file 消息发送：本地拒绝"), and the group envelope accepts only
//     TEXT / IMAGE / MD bodies. So anything that is not an image travels as a
//     Markdown message whose body is a link to the freshly uploaded object.
//     That is a real delivery — the recipient taps it and downloads the file —
//     but it is a link rather than a file card, and the paragraph above is why.
//
// The answer goes first, always. An upload is megabytes and round trips and it
// can fail; the sentence the agent wrote cannot be made to wait behind one, and
// must not be lost to one. So delivery runs after the reply is out, on its own
// goroutine and its own budget, and its worst outcome is one extra line saying
// a file did not make it.
//
// Every failure is best-effort and local: the object stays in object storage,
// the attachment row stays where it is, and the user can ask again.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dolfly/infoflow"
	"github.com/dolfly/infoflow/bot/im"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
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

// mediaSender is the slice of an InfoFlow client this path uses. It exists so a
// test can watch what would have gone on the wire without a network, and so the
// delivery logic below is a function of its inputs rather than of a live robot.
type mediaSender interface {
	upload(ctx context.Context, data []byte, name string) (im.UploadFileResult, error)
	sendImage(ctx context.Context, target sendTarget, objectKey string) (channel.SendResult, error)
	sendMarkdown(ctx context.Context, target sendTarget, content string) (channel.SendResult, error)
	sendText(ctx context.Context, target sendTarget, text string) (channel.SendResult, error)
}

// sdkMediaSender is the production mediaSender: every call is one InfoFlow API
// request through the robot's own credentials.
type sdkMediaSender struct {
	client *infoflow.Client
}

func (s sdkMediaSender) upload(ctx context.Context, data []byte, name string) (im.UploadFileResult, error) {
	return s.client.Bot.IM.File.Upload(ctx, im.UploadFileOptions{
		File:              data,
		FileName:          name,
		ExpirationSeconds: outboundUploadExpirationSeconds,
	})
}

func (s sdkMediaSender) sendImage(ctx context.Context, target sendTarget, objectKey string) (channel.SendResult, error) {
	return sendImage(ctx, s.client, target, objectKey)
}

func (s sdkMediaSender) sendMarkdown(ctx context.Context, target sendTarget, content string) (channel.SendResult, error) {
	return sendMarkdown(ctx, s.client, target, content)
}

func (s sdkMediaSender) sendText(ctx context.Context, target sendTarget, text string) (channel.SendResult, error) {
	return sendText(ctx, s.client, target, text)
}

// newSenderFor builds the production sender from decrypted credentials.
func newSenderFor(creds credentials) (mediaSender, error) {
	client, err := newSDKClient(creds)
	if err != nil {
		return nil, err
	}
	return sdkMediaSender{client: client}, nil
}

const (
	// maxOutboundBytes caps one uploaded object.
	//
	// InfoFlow's BOS gateway takes large objects, but this number is not the
	// platform's ceiling — it is this process's. Every delivery reads its object
	// fully into memory to hand it to File.Upload, so the cap is what bounds the
	// heap one reply can cost. Generous: a 20 MiB attachment is already past
	// what any chat needs.
	maxOutboundBytes = 20 << 20

	// outboundUploadExpirationSeconds is the lifetime of the presigned URL a
	// non-image file is delivered as. The platform maximum (86400) is used
	// because the link is the message: a one-hour link would leave the user
	// holding a dead file long before the conversation is over. The URL is a
	// bearer-like value — it is sent to the recipient and never logged.
	outboundUploadExpirationSeconds = 86400

	// attachmentDeliveryBudget bounds one turn's whole attachment delivery —
	// reading every object, uploading it and sending it.
	attachmentDeliveryBudget = 3 * time.Minute

	// maxConcurrentAttachmentDeliveries caps how many turns deliver
	// attachments at once. Process-wide, not per installation: the heap is
	// process-wide, so a per-installation cap on a deployment running several
	// robots just multiplies.
	maxConcurrentAttachmentDeliveries = 2
)

// attachmentSlots bounds concurrent deliveries. A buffered channel of struct{}
// is the semaphore; a send that cannot proceed is dropped, not queued.
var attachmentSlots = make(chan struct{}, maxConcurrentAttachmentDeliveries)

// OutboundOption configures the chat-done subscriber at construction.
type OutboundOption func(*Outbound)

// WithAttachments turns on file delivery. Without it — a deployment with no
// object storage — a turn is delivered exactly as it was before, and the agent
// is not told files can travel (router.go pairs this with
// Handler.DeclareChannelFileDelivery so the claim and the capability cannot
// drift apart).
func WithAttachments(objects mediaObjectStore) OutboundOption {
	return func(o *Outbound) { o.objects = objects }
}

// attachmentNotice is what the user is told when a file the agent produced did
// not reach the conversation. English, like every other user-facing string this
// adapter sends (replier.go).
const attachmentNotice = "⚠️ Some files from that reply could not be delivered to InfoFlow."

// deliverAttachments carries every file bound to one completed turn into the
// conversation. It is synchronous so a test can call it directly;
// spawnAttachmentDelivery puts it on a goroutine of its own.
func (o *Outbound) deliverAttachments(ctx context.Context, sender mediaSender, target sendTarget, workspaceID, messageID, installationID pgtype.UUID) {
	rows, err := o.q.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID,
		WorkspaceID:   workspaceID,
	})
	if err != nil {
		o.logger.WarnContext(ctx, "infoflow outbound: attachment lookup failed",
			"error", err, "installation_id", util.UUIDToString(installationID))
		return
	}
	failed := 0
	for _, row := range rows {
		if err := o.sendAttachment(ctx, sender, target, row); err != nil {
			failed++
			// The object's URL stays out of the log: it is an address that
			// serves the file to whoever holds it.
			o.logger.WarnContext(ctx, "infoflow outbound: attachment not delivered",
				"error", err,
				"installation_id", util.UUIDToString(installationID),
				"attachment_id", util.UUIDToString(row.ID),
				"content_type", row.ContentType,
				"size_bytes", row.SizeBytes)
		}
	}
	if failed > 0 {
		if _, err := sender.sendText(ctx, target, attachmentNotice); err != nil {
			o.logger.WarnContext(ctx, "infoflow outbound: could not tell the user about the file",
				"error", err, "installation_id", util.UUIDToString(installationID))
		}
	}
}

// sendAttachment carries one file from object storage into the chat.
//
// Images go as an image message keyed by the uploaded BOS object key. Anything
// else goes as a Markdown link to the uploaded object, because the platform has
// no file message for a bot to send (see the file header).
func (o *Outbound) sendAttachment(ctx context.Context, sender mediaSender, target sendTarget, row db.Attachment) error {
	data, err := o.readObject(ctx, row.Url)
	if err != nil {
		return err
	}
	name := outboundMediaName(row.Filename, row.ContentType)
	if name == "" {
		// File.Upload requires a name without a path separator; a blank one
		// would be refused by the SDK before any byte leaves.
		return errors.New("infoflow: attachment has no usable filename")
	}
	uploaded, err := sender.upload(ctx, data, name)
	if err != nil {
		return fmt.Errorf("upload attachment: %w", err)
	}
	if isImageAttachment(row.ContentType, name) {
		if _, err := sender.sendImage(ctx, target, uploaded.ObjectKey); err != nil {
			return fmt.Errorf("send image message: %w", err)
		}
		return nil
	}
	if strings.TrimSpace(uploaded.URL) == "" {
		return errors.New("infoflow: upload returned no URL to link the file to")
	}
	if _, err := sender.sendMarkdown(ctx, target, fileLinkMarkdown(name, uploaded.URL)); err != nil {
		return fmt.Errorf("send file link: %w", err)
	}
	return nil
}

// readObject fetches one attachment's bytes, bounded by maxOutboundBytes.
func (o *Outbound) readObject(ctx context.Context, rawURL string) ([]byte, error) {
	if o.objects == nil {
		return nil, errors.New("infoflow: attachment delivery is not configured")
	}
	key := o.objects.KeyFromURL(rawURL)
	if key == "" {
		return nil, errors.New("infoflow: attachment is not an object this deployment stores")
	}
	rc, err := o.objects.GetReader(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	defer rc.Close()
	// One byte of headroom, so reading exactly the cap can be told from a file
	// that has more to come.
	data, err := io.ReadAll(io.LimitReader(rc, maxOutboundBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if len(data) > maxOutboundBytes {
		return nil, fmt.Errorf("infoflow: attachment is larger than the %d byte limit", maxOutboundBytes)
	}
	return data, nil
}

// outboundMediaName is what InfoFlow is told the upload is called, and what the
// recipient sees on the link. It is reduced to a single path segment — the name
// reaches the wire and a stored filename is not guaranteed to be one — and given
// an extension when it has none, since the extension is the only hint a link
// carries about the format.
func outboundMediaName(filename, contentType string) string {
	name := strings.TrimSpace(filename)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.TrimRight(name, "\r\n")
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	if path.Ext(name) == "" {
		if ext := outboundMediaExtension(baseContentType(contentType)); ext != "" {
			name += ext
		}
	}
	return name
}

// baseContentType strips parameters from a content type.
func baseContentType(raw string) string {
	if semi := strings.IndexByte(raw, ';'); semi >= 0 {
		raw = raw[:semi]
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// isImageAttachment decides whether this file travels as an image message.
//
// The content type leads, because it is what the uploader declared. When it
// says nothing useful — empty, or the octet-stream that means "bytes" — the
// filename's extension is the better guess.
func isImageAttachment(contentType, filename string) bool {
	ct := baseContentType(contentType)
	if ct == "" || ct == "application/octet-stream" {
		ct = baseContentType(mime.TypeByExtension(path.Ext(filename)))
	}
	return strings.HasPrefix(ct, "image/")
}

// outboundMediaExtension maps a content type to a file extension, so a file
// uploaded without a name still carries a format hint.
func outboundMediaExtension(contentType string) string {
	switch contentType {
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
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/zip":
		return ".zip"
	case "application/json":
		return ".json"
	default:
		return ""
	}
}

// fileLinkMarkdown renders the Markdown body that delivers a non-image file.
// The label is escaped so a filename carrying brackets cannot rewrite the link.
func fileLinkMarkdown(name, url string) string {
	return "[" + escapeMarkdownLabel(name) + "](" + strings.TrimSpace(url) + ")"
}

// escapeMarkdownLabel neutralizes the three characters that can break out of a
// Markdown link label. Backslash first, or it would escape the backslashes the
// next two replacements add.
func escapeMarkdownLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "[", `\[`)
	s = strings.ReplaceAll(s, "]", `\]`)
	return s
}

// sendImage delivers one image message through a client, choosing the user or
// group endpoint from the target kind. The content is the BOS object key, which
// is what both envelopes call an image's content.
func sendImage(ctx context.Context, client *infoflow.Client, target sendTarget, objectKey string) (channel.SendResult, error) {
	if strings.TrimSpace(objectKey) == "" {
		return channel.SendResult{}, errors.New("infoflow: empty image key")
	}
	switch target.ChatType {
	case channel.ChatTypeGroup:
		groupID, err := groupIDInt64(target.ChatID)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("infoflow: invalid group id %q: %w", target.ChatID, err)
		}
		res, err := client.Bot.IM.Message.SendGroupMessage(ctx, im.SendGroupMessageRequest{
			GroupID: groupID,
			Type:    "IMAGE",
			Body:    []im.GroupBody{{Type: "IMAGE", Content: objectKey}},
		})
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageID, 10)}, nil
	default:
		res, err := client.Bot.IM.Message.SendUserMessage(ctx, im.SendUserMessageRequest{
			ToUser: target.ChatID,
			Type:   "image",
			Image:  objectKey,
		})
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageKey, 10)}, nil
	}
}

// sendMarkdown delivers one Markdown message through a client. InfoFlow's group
// envelope accepts only TEXT / IMAGE / MD bodies, so MD is the richest thing a
// group can carry; a private chat accepts the same type.
func sendMarkdown(ctx context.Context, client *infoflow.Client, target sendTarget, content string) (channel.SendResult, error) {
	if strings.TrimSpace(content) == "" {
		return channel.SendResult{}, errors.New("infoflow: empty markdown message")
	}
	switch target.ChatType {
	case channel.ChatTypeGroup:
		groupID, err := groupIDInt64(target.ChatID)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("infoflow: invalid group id %q: %w", target.ChatID, err)
		}
		res, err := client.Bot.IM.Message.SendGroupMessage(ctx, im.SendGroupMessageRequest{
			GroupID: groupID,
			Type:    "MD",
			Body:    []im.GroupBody{{Type: "MD", Content: content}},
		})
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageID, 10)}, nil
	default:
		res, err := client.Bot.IM.Message.SendUserMessage(ctx, im.SendUserMessageRequest{
			ToUser: target.ChatID,
			Type:   "md",
			Text:   content,
		})
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageKey, 10)}, nil
	}
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

// mayCarryAttachments reports whether a turn is worth the lookups. Everything
// it checks is already in hand, so a deployment with no storage costs nothing.
func (o *Outbound) mayCarryAttachments(e events.Event) bool {
	return o.objects != nil && e.Type == protocol.EventChatDone && chatDoneMessageID(e.Payload) != ""
}

// spawnAttachmentDelivery hands one turn's files to a goroutine of their own, if
// admission allows. It returns immediately; the caller is on the publish path,
// which must never wait on an upload.
func (o *Outbound) spawnAttachmentDelivery(creds credentials, target sendTarget, workspaceID, messageID, installationID pgtype.UUID) {
	if o.objects == nil {
		return
	}
	sender, err := o.senderFor(creds)
	if err != nil {
		o.logger.Warn("infoflow outbound: cannot build a sender for attachment delivery",
			"error", err, "installation_id", util.UUIDToString(installationID))
		return
	}
	select {
	case attachmentSlots <- struct{}{}:
	default:
		o.logger.Warn("infoflow outbound: attachment delivery not admitted, too many already running",
			"installation_id", util.UUIDToString(installationID),
			"admitted", maxConcurrentAttachmentDeliveries)
		return
	}
	o.attachmentWG.Add(1)
	go func() {
		defer o.attachmentWG.Done()
		defer func() { <-attachmentSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), attachmentDeliveryBudget)
		defer cancel()
		o.deliverAttachments(ctx, sender, target, workspaceID, messageID, installationID)
	}()
}

package lark

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Based on multica-ai/multica#8332, commit 95756b4f0f98d620278619029fae863fa90bc680.
// Text is delivered first, then uploaded images update that same card; files
// are separate messages. Only server-bound attachments are read; response
// Markdown and provider URLs are never treated as authority to fetch a file.
type mediaAPIClient interface {
	UploadImage(context.Context, UploadImageParams) (string, error)
	UploadFile(context.Context, UploadFileParams) (string, error)
	SendImageMessage(context.Context, SendImageParams) (string, error)
	SendFileMessage(context.Context, SendFileParams) (string, error)
}
type mediaObjectStore interface {
	KeyFromURL(string) string
	GetReader(context.Context, string) (io.ReadCloser, error)
}

const (
	mediaSendFailedText               = "部分附件未能发送到飞书，仍保留在 Multica 的这条回复中。"
	mediaSendUnknownText              = "无法确认部分附件是否已送达飞书，请先检查当前会话；附件仍保留在 Multica 中。"
	mediaLookupFailedText             = "暂时无法检查这条回复的附件；如有附件，请在 Multica 中查看。"
	attachmentBudget                  = 5 * time.Minute
	attachmentNoticeBudget            = 5 * time.Second
	maxConcurrentAttachmentDeliveries = 2
	maxAdmittedAttachmentDeliveries   = 64
	maxPendingAttachmentDeliveries    = 32
)

// SetAttachments returns the same capability verdict announced on task claim.
func (p *Patcher) SetAttachments(objects mediaObjectStore) bool {
	p.objects = objects
	return objects != nil && p.media != nil && p.client.IsConfigured()
}

func chatDoneMessageID(payload any) string {
	switch v := payload.(type) {
	case protocol.ChatDonePayload:
		return v.MessageID
	case map[string]any:
		s, _ := v["message_id"].(string)
		return s
	}
	return ""
}
func chatDoneTaskID(payload any) string {
	switch v := payload.(type) {
	case protocol.ChatDonePayload:
		return v.TaskID
	case map[string]any:
		s, _ := v["task_id"].(string)
		return s
	}
	return ""
}

// Admission precedes both goroutine creation and the database lookup. Pending
// deliveries are bounded separately, after a lookup proves files exist.
func (p *Patcher) deliverAttachmentsAsync(parent context.Context, creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, workspaceID pgtype.UUID, payload any) bool {
	if p.objects == nil || p.media == nil {
		return false
	}
	messageID, err := util.ParseUUID(chatDoneMessageID(payload))
	if err != nil || !messageID.Valid || !workspaceID.Valid {
		return false
	}
	taskID, err := util.ParseUUID(chatDoneTaskID(payload))
	if err != nil || !taskID.Valid {
		return false
	}
	select {
	case p.mediaAdmission <- struct{}{}:
	default:
		p.cfg.Logger.Warn("lark outbound: attachment lookup admission full", "message_id", uuidString(messageID))
		return false
	}
	p.spawn(func() {
		defer func() { <-p.mediaAdmission }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), p.mediaBudget)
		defer cancel()
		if _, ok := ctx.Value(mediaDeliveryContextKey{}).(*mediaDeliveryOwner); !ok {
			var acquired bool
			ctx, _, acquired = p.claimMediaDelivery(ctx, taskID, binding)
			if !acquired {
				return
			}
		}
		defer p.finishMediaDelivery(ctx)
		p.deliverAttachments(ctx, creds, binding, target, workspaceID, uuidString(messageID))
	})
	return true
}

func (p *Patcher) deliverAttachments(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, workspaceID pgtype.UUID, rawMessageID string) {
	ctx = context.WithValue(ctx, mediaWorkspaceContextKey{}, workspaceID)
	messageID, err := util.ParseUUID(rawMessageID)
	if err != nil || !messageID.Valid {
		return
	}
	rows, err := p.queries.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID, WorkspaceID: workspaceID,
	})
	if err != nil {
		p.cfg.Logger.Warn("lark outbound: attachment lookup failed", "message_id", rawMessageID, "error", err)
		p.tellUser(ctx, creds, binding, target, mediaLookupFailedText)
		return
	}
	if len(rows) == 0 {
		return
	}
	select {
	case p.mediaPending <- struct{}{}:
		defer func() { <-p.mediaPending }()
	default:
		p.tellUser(ctx, creds, binding, target, mediaSendFailedText)
		return
	}
	select {
	case p.mediaSlots <- struct{}{}:
		defer func() { <-p.mediaSlots }()
	case <-ctx.Done():
		p.tellUser(ctx, creds, binding, target, mediaSendFailedText)
		return
	}
	if !p.markMediaSending(ctx) {
		// A previous owner may have sent files. Never repeat an uncertain batch.
		p.tellUser(ctx, creds, binding, target, mediaSendUnknownText)
		return
	}
	failed, unknown := 0, 0
	for _, row := range rows {
		if err := p.deliverOneAttachment(ctx, creds, binding, &target, workspaceID, messageID, row); err != nil {
			var uncertain *mediaSendUnconfirmed
			if errors.As(err, &uncertain) {
				unknown++
			} else {
				failed++
			}
			// Object URLs and filenames can contain private data.
			p.cfg.Logger.Warn("lark outbound: attachment delivery unconfirmed",
				"attachment_id", uuidString(row.ID), "uncertain", uncertain != nil)
		}
	}
	var notices []string
	if failed > 0 {
		notices = append(notices, mediaSendFailedText)
	}
	if unknown > 0 {
		notices = append(notices, mediaSendUnknownText)
	}
	if len(notices) > 0 {
		p.tellUser(ctx, creds, binding, target, strings.Join(notices, "\n"))
	}
}

type mediaSendUnconfirmed struct{ err error }

func (e *mediaSendUnconfirmed) Error() string { return "lark media send was not confirmed" }
func (e *mediaSendUnconfirmed) Unwrap() error { return e.err }

// An explicit refusal proves the message was not accepted. A lost response,
// missing message id, or 5xx does not. Only the latter uses a bounded retry,
// with exactly the same provider idempotency key.
func definiteMediaRefusal(err error) bool {
	// A message still being sent has no confirmed outcome, even under HTTP 400.
	if larkErrorCode(err) == 230049 {
		return false
	}
	var status *larkAPIStatusError
	if errors.As(err, &status) {
		return status.StatusCode >= 400 && status.StatusCode < 500
	}
	var api *APIError
	return errors.As(err, &api) && api.Code != 0
}

func (p *Patcher) deliverOneAttachment(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, target *ReplyTarget, workspaceID, messageID pgtype.UUID, row db.Attachment) error {
	if row.SizeBytes > larkFileUploadMaxBytes {
		return errors.New("attachment exceeds file limit")
	}
	var err error
	creds, err = p.currentMediaCredentials(ctx, creds, binding, workspaceID)
	if err != nil {
		return err
	}
	key := p.objects.KeyFromURL(strings.TrimSpace(row.Url))
	if key == "" {
		return errors.New("attachment is not stored by this deployment")
	}
	rc, err := p.objects.GetReader(ctx, key)
	if err != nil {
		return errors.New("attachment object unavailable")
	}
	// A cancelled delivery must also interrupt a storage implementation whose
	// reader only unblocks when closed.
	stop := context.AfterFunc(ctx, func() { _ = rc.Close() })
	defer stop()
	data, readErr := io.ReadAll(io.LimitReader(rc, larkFileUploadMaxBytes+1))
	_ = rc.Close()
	if readErr != nil {
		return errors.New("attachment object read failed")
	}
	if len(data) == 0 || len(data) > larkFileUploadMaxBytes {
		return errors.New("attachment size invalid")
	}
	creds, err = p.currentMediaCredentials(ctx, creds, binding, workspaceID)
	if err != nil {
		return err
	}
	image := isImageAttachment(row.ContentType, row.Filename) && len(data) <= larkImageUploadMaxBytes
	fileType := larkFileTypeFor(row.Filename)
	var resourceKey string
	if image {
		resourceKey, err = p.media.UploadImage(ctx, UploadImageParams{InstallationID: creds, Filename: row.Filename, Data: data})
		if err != nil && definiteMediaRefusal(err) && !isTokenError(larkErrorCode(err)) {
			image = false
		}
	}
	if !image {
		creds, err = p.currentMediaCredentials(ctx, creds, binding, workspaceID)
		if err != nil {
			return err
		}
		resourceKey, err = p.media.UploadFile(ctx, UploadFileParams{InstallationID: creds, Filename: row.Filename, FileType: fileType, Data: data})
	}
	if err != nil {
		return errors.New("attachment upload failed")
	}
	if image {
		if card, ok := ctx.Value(mediaReplyCardContextKey{}).(*mediaReplyCard); ok {
			return p.patchMediaReplyCard(ctx, creds, binding, workspaceID, card, row, resourceKey)
		}
	}
	// Scope the key to the installation, reply and attachment, not just a file:
	// deliberate reuse of one file in a later reply must remain a new send.
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(uuidString(binding.InstallationID)+"/"+uuidString(messageID)+"/"+uuidString(row.ID))).String()
	send := func(t ReplyTarget) error {
		uncertain := false
		for attempt := 0; attempt < 2; attempt++ {
			current, guardErr := p.currentMediaCredentials(ctx, creds, binding, workspaceID)
			if guardErr != nil {
				if uncertain {
					return &mediaSendUnconfirmed{err: guardErr}
				}
				return guardErr
			}
			callCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
			if image {
				_, err = p.media.SendImageMessage(callCtx, SendImageParams{InstallationID: current, ChatID: outboundChatID(binding), ImageKey: resourceKey, ReplyTarget: t, UUID: id})
			} else {
				_, err = p.media.SendFileMessage(callCtx, SendFileParams{InstallationID: current, ChatID: outboundChatID(binding), FileKey: resourceKey, FileType: fileType, ReplyTarget: t, UUID: id})
			}
			cancel()
			if err == nil {
				*target = t
				return nil
			}
			if definiteMediaRefusal(err) {
				if uncertain {
					return &mediaSendUnconfirmed{err: err}
				}
				return err
			}
			uncertain = true
			if ctx.Err() != nil {
				break
			}
		}
		return &mediaSendUnconfirmed{err: err}
	}
	return sendWithReplyFallback(p.cfg.Logger, "send attachment", *target, send)
}

// Notices outlive the failed transfer budget, but never a revoked connection.
func (p *Patcher) tellUser(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, target ReplyTarget, body string) {
	noticeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachmentNoticeBudget)
	defer cancel()
	current, err := p.currentMediaCredentials(noticeCtx, creds, binding, pgtype.UUID{})
	if err != nil {
		return
	}
	if _, err := p.client.SendTextMessage(noticeCtx, SendTextParams{
		InstallationID: current, ChatID: outboundChatID(binding),
		Text: prependTextMention(mentionOpenID(binding), body), ReplyTarget: target,
	}); err != nil {
		p.cfg.Logger.Warn("lark outbound: attachment notice failed")
	}
}

func isImageAttachment(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if ct == "" || ct == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".png":
			ct = "image/png"
		case ".gif":
			ct = "image/gif"
		case ".webp":
			ct = "image/webp"
		case ".bmp":
			ct = "image/bmp"
		case ".ico":
			ct = "image/x-icon"
		case ".tif", ".tiff":
			ct = "image/tiff"
		case ".heic":
			ct = "image/heic"
		}
	}
	switch ct {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp",
		"image/x-icon", "image/vnd.microsoft.icon", "image/tiff", "image/heic":
		return true
	}
	return false
}

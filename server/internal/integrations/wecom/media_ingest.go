package wecom

// media_ingest.go — the engine.MediaResolver for WeCom.
//
// The shape is the one lark/media_ingest.go established and the Router
// depends on: HasMedia is a pure in-memory look at the payload we already
// have, ResolveMedia runs detached from the connector ACK path, every upload
// is covered by an intent-ledger row written BEFORE the PUT, and nothing is
// ever deleted inline — a failure anywhere leaves the row for the reconciler
// and leaves the message's placeholder text intact.
//
// What is wecom-specific is the middle: WeCom hands over a pre-signed COS url
// and a per-url key instead of a resource id to be fetched with a tenant
// token, so there is no API client and no credential here — just an HTTP GET
// and a decrypt (media_download.go, media_crypt.go). The callback body says
// nothing about the file besides those two strings, so the name comes out of
// the download's Content-Disposition and the type is worked out from the name
// or sniffed from the decrypted bytes.

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
	"path"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// mediaFailureNotice lines. WeCom deployments are China-only, so these follow
// the Chinese product voice the rest of this adapter already writes in
// (wecom_channel.go's receipt, router.go's session titles).
const (
	mediaUnreadableNotice = "抱歉，有附件没能收到，麻烦重新发一次。"
	mediaTooLargeNotice   = "抱歉，附件太大了，我这边收不下。"
)

// mediaStorage is the slice of storage.Storage this resolver drives.
// ObjectURL is a pure function of configuration, which is what lets the
// intent ledger persist the object's URL before the object exists.
type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	ObjectURL(key string) string
}

// mediaStreamStorage is the streaming half, which both production backends
// implement (storage.S3Storage, storage.LocalStorage). A backend without it
// falls back to the buffered path — correct, just not memory-flat.
type mediaStreamStorage interface {
	UploadStream(ctx context.Context, key string, data io.Reader, sizeBytes int64, contentType string, filename string) (string, error)
}

type wecomMediaResolver struct {
	storage mediaStorage
	ledger  engine.MediaIntentLedger
	http    *http.Client
	// notify reaches the live aibot socket for the installation, to tell the
	// sender an attachment did not make it. nil disables the notice and
	// leaves only the log.
	notify *sendersRegistry
	logger *slog.Logger
}

// NewMediaResolver builds the wecom MediaResolver. storage and ledger are
// required — without either there is nothing durable to point an attachment
// at, and the resolver degrades to leaving the placeholder in place. senders
// is optional: without it a failed attachment is only logged.
func NewMediaResolver(storage mediaStorage, ledger engine.MediaIntentLedger, senders *sendersRegistry, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &wecomMediaResolver{
		storage: storage,
		ledger:  ledger,
		// Never a bare http.Client: the URL being fetched came off the wire,
		// and this client refuses to connect to anything that is not public
		// internet (media_guard.go).
		http:   newMediaHTTPClient(mediaGuard{}),
		notify: senders,
		logger: logger,
	}
}

// HasMedia reports whether this callback carried anything to download. It
// runs synchronously on the connector ACK path and decides whether the
// message pays for a media deadline, a deferred run and a semaphore slot at
// all, so it stays a decode of bytes already in hand.
func (r *wecomMediaResolver) HasMedia(msg channel.InboundMessage) bool {
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		return false
	}
	return len(wm.Media) > 0
}

// ResolveMedia downloads, decrypts and stores every attachment on the
// message, returning it with a MediaRef per object that landed. Attachments
// are independent: one that fails does not stop the rest, and the sender is
// told once at the end about whatever did not arrive.
func (r *wecomMediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		r.logger.Warn("wecom media ingest skipped: raw decode failed", "message_id", msg.MessageID, "err", err)
		return msg
	}
	if len(wm.Media) == 0 {
		return msg
	}
	if r.storage == nil || r.ledger == nil {
		r.logger.Warn("wecom media ingest skipped: no storage configured",
			"msg_id", wm.MsgID, "attachments", len(wm.Media))
		return msg
	}

	var failures []mediaFailure
	for i, m := range wm.Media {
		ref, err := r.ingestOne(ctx, inst, chatMessageID, wm, i, m)
		if err != nil {
			failure := classifyMediaFailure(err)
			failures = appendFailure(failures, failure)
			// A refused address gets its own line because the operator's next
			// move is different from every other failure here: the attachment
			// is fine and the deployment declined to dial where its host
			// resolved, so the fix is configuration, not a retry. Saying only
			// "ingest failed" for this sends them looking at WeCom.
			logLine := "wecom media ingest failed"
			if failure == mediaFailureBlocked {
				logLine = "wecom media ingest refused by the media address guard: the host resolved to a non-public address and was not dialed. If this deployment sits behind a fake-IP proxy, declare its range in MULTICA_WECOM_MEDIA_ALLOW_CIDRS; otherwise this is a URL that should not have been sent."
			}
			// The url and the key never reach the log: one is a signed
			// address anyone could then fetch, the other unlocks it. stripURL
			// has already taken the url out of err, and the guard's refusal
			// carries no address of its own, so err is safe to log whole.
			r.logger.Warn(logLine,
				"installation_id", util.UUIDToString(inst.ID),
				"msg_id", wm.MsgID,
				"attachment", i,
				"kind", string(m.Kind),
				"err", err)
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}

	r.tellTheSender(inst, wm, failures)
	return msg
}

// ingestOne carries a single attachment from url to MediaRef. The ledger row
// goes first: from that point on every failure — download, decrypt, upload,
// a crash — leaves an intent the reconciler settles, and nothing here deletes
// anything.
func (r *wecomMediaResolver) ingestOne(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, wm InboundMessage, index int, m InboundMedia) (channel.MediaRef, error) {
	key := mediaObjectKey(inst, chatMessageID, wm.MsgID, index, m.Kind)
	link := r.storage.ObjectURL(key)

	ok, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{
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
	if !ok {
		// The reconciler owns this key; never resurrect it.
		return channel.MediaRef{}, fmt.Errorf("media key %s is owned by the reconciler", key)
	}

	streamer, canStream := r.storage.(mediaStreamStorage)
	if canStream {
		// The memory-flat path: ciphertext streams from the socket into the
		// decrypt, the plaintext lands in an unlinked temp file, and the
		// upload reads from there. Nothing holds a whole attachment.
		ref, err := r.ingestStreaming(ctx, streamer, wm, index, m, key, link)
		if err == nil || !errors.Is(err, errStreamingUnavailable) {
			return ref, err
		}
		// Fall through: the backend said it could stream and then could not.
	}

	got, err := downloadMedia(ctx, r.http, m.URL)
	if err != nil {
		return channel.MediaRef{}, err
	}
	traceMediaHeaders(r.logger, wm.MsgID, index, got.mediaHeaders)
	plain, err := decryptMedia(m.AESKey, got.Body)
	if err != nil {
		return channel.MediaRef{}, err
	}

	filename, contentType := describeMedia(wm, index, m, got.Filename, plain)
	if _, err := r.storage.Upload(ctx, key, plain, contentType, filename); err != nil {
		// The store may still be processing the PUT; deleting here could
		// reorder with it. The intent row covers the object either way.
		return channel.MediaRef{}, fmt.Errorf("upload media: %w", err)
	}
	return channel.MediaRef{
		Type:       m.Kind,
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  int64(len(plain)),
	}, nil
}

// mediaObjectKey names the object. It is derived from the CHAT message rather
// than the WeCom message for the reason lark documents: one platform message
// can be ingested twice (the inbound dedup claim is reclaimable once stale),
// and a shared key would run the second ingest into the first one's ledger
// row — possibly a tombstone the intent upsert refuses, silently dropping the
// media. The attachment's position in the message is part of the key too,
// since a 图文混排 can carry several and the url is not stable enough to key on.
func mediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, msgID string, index int, kind channel.MsgType) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d",
		util.UUIDToString(chatMessageID), msgID, string(kind), index)))
	return path.Join(
		"workspaces",
		util.UUIDToString(inst.WorkspaceID),
		"wecom",
		util.UUIDToString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
}

// describeMedia works out what to call the file and what to say it is. The
// callback body carries neither, so the name comes from the download's
// Content-Disposition and the type from the name's extension, falling back to
// sniffing the decrypted bytes — the extension is the better signal for the
// formats that are really zip containers (.docx, .xlsx), sniffing is the
// better one when there is no name at all.
func describeMedia(wm InboundMessage, index int, m InboundMedia, headerName string, plain []byte) (filename, contentType string) {
	filename = cleanMediaFilename(headerName)
	if ext := path.Ext(filename); ext != "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = http.DetectContentType(plain)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if filename == "" {
		filename = fallbackMediaName(wm.MsgID, index, m.Kind, contentType)
	}
	return filename, contentType
}

// fallbackMediaName builds a name for an attachment the server did not name.
// It has to be unique within the message, so the attachment's position is in
// it — two photos in one 图文混排 otherwise land as one name twice.
func fallbackMediaName(msgID string, index int, kind channel.MsgType, contentType string) string {
	prefix := "wecom-file"
	switch kind {
	case channel.MsgTypeImage:
		prefix = "wecom-image"
	case channel.MsgTypeVideo:
		prefix = "wecom-video"
	}
	return fmt.Sprintf("%s-%s-%d%s", prefix, safeMediaSegment(msgID), index, mediaExtension(contentType))
}

// mediaExtension picks a file extension for a content type, preferring the
// familiar spelling over whatever the mime database happens to list first
// (image/jpeg resolves to ".jfif" on some systems).
func mediaExtension(contentType string) string {
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
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

// ---- telling the sender ----

// mediaFailure is the kind of bad news, not the error itself: the sender gets
// told what to do differently, and "too big" and "did not arrive" have
// different answers.
type mediaFailure int

const (
	mediaFailureUnreadable mediaFailure = iota
	mediaFailureTooLarge
	// mediaFailureBlocked is the guard refusing the address the media host
	// resolved to. It is separated from mediaFailureUnreadable for the
	// OPERATOR, not the sender: nothing is wrong with the attachment, and no
	// number of retries will help until the deployment's own configuration
	// changes. See the log branch in ResolveMedia.
	mediaFailureBlocked
)

func classifyMediaFailure(err error) mediaFailure {
	if errors.Is(err, errMediaTooLarge) {
		return mediaFailureTooLarge
	}
	if errors.Is(err, ErrMediaAddrBlocked) {
		return mediaFailureBlocked
	}
	return mediaFailureUnreadable
}

// appendFailure keeps one entry per kind. A message with four attachments
// that all expired is one thing that went wrong, and saying it four times
// helps nobody.
func appendFailure(list []mediaFailure, f mediaFailure) []mediaFailure {
	for _, existing := range list {
		if existing == f {
			return list
		}
	}
	return append(list, f)
}

// tellTheSender writes one short notice into the chat the attachments came
// from, over the same live aibot socket every other wecom message uses.
//
// Without it the failure is invisible in the worst way: the stored body still
// says [Image], so the agent answers as if it had been shown a picture it
// never received. The agent run itself is deliberately left to proceed — the
// person can see for themselves that the picture did not get through, and the
// answer to whatever they typed beside it is still worth having.
func (r *wecomMediaResolver) tellTheSender(inst engine.ResolvedInstallation, wm InboundMessage, failures []mediaFailure) {
	if len(failures) == 0 || r.notify == nil || !inst.ID.Valid {
		return
	}
	chatID := wm.ChatID
	if chatID == "" {
		chatID = wm.SenderUserID
	}
	if chatID == "" {
		return
	}
	sender := r.notify.get(inst.ID)
	if sender == nil {
		// Mid-reconnect: the socket this installation writes over does not
		// exist right now. The log below is the whole record.
		r.logger.Warn("wecom media failure notice not delivered: no live connection",
			"installation_id", util.UUIDToString(inst.ID), "msg_id", wm.MsgID)
		return
	}
	chatType := chatTypeSingleInt
	if strings.EqualFold(wm.ChatType, "group") {
		chatType = chatTypeGroupInt
	}
	lines := make([]string, 0, len(failures))
	for _, f := range failures {
		notice := mediaUnreadableNotice
		if f == mediaFailureTooLarge {
			notice = mediaTooLargeNotice
		}
		// mediaFailureBlocked lands on the unreadable wording deliberately.
		// From the sender's side a refused address and a download that fell
		// over are the same event — the attachment did not arrive — and the
		// thing that separates them is what the operator must change, which
		// belongs in the log. Neither the resolved address nor the signed url
		// goes into a chat message.
		//
		// Two kinds sharing one wording is why the dedupe moved here from
		// appendFailure: a message with one blocked and one unreadable
		// attachment is still one piece of news.
		if !slices.Contains(lines, notice) {
			lines = append(lines, notice)
		}
	}
	if err := sender.sendText(chatID, chatType, strings.Join(lines, "\n")); err != nil {
		r.logger.Warn("wecom media failure notice not delivered",
			"installation_id", util.UUIDToString(inst.ID), "msg_id", wm.MsgID, "err", err)
	}
}

// errStreamingUnavailable says the memory-flat path could not be taken for a
// reason that is not the attachment's fault — no temp space, a backend that
// advertises UploadStream and rejects it. The caller falls back to the
// buffered path rather than failing an attachment over an optimisation.
var errStreamingUnavailable = errors.New("wecom: streaming media ingest unavailable")

// ingestStreaming carries one attachment without ever holding it whole:
// ciphertext streams from the response body into the decrypt, the plaintext
// lands in an unlinked temp file, and the upload reads from that file with
// the exact length it now knows.
func (r *wecomMediaResolver) ingestStreaming(
	ctx context.Context,
	streamer mediaStreamStorage,
	wm InboundMessage,
	index int,
	m InboundMedia,
	key, link string,
) (channel.MediaRef, error) {
	body, headers, err := openMedia(ctx, r.http, m.URL)
	if err != nil {
		return channel.MediaRef{}, err
	}
	defer body.Close()
	traceMediaHeaders(r.logger, wm.MsgID, index, headers)

	plain, size, err := decryptToFile(m.AESKey, body, "")
	if err != nil {
		// A decrypt failure is the attachment's problem and must be reported
		// as one; only a failure to make the temp file is grounds to fall
		// back, and decryptToFile says so in its message.
		if strings.Contains(err.Error(), "media temp file") {
			return channel.MediaRef{}, fmt.Errorf("%w: %v", errStreamingUnavailable, err)
		}
		return channel.MediaRef{}, err
	}
	defer plain.Close()

	// The type is sniffed from the head of the file rather than from the
	// whole thing — http.DetectContentType only ever reads 512 bytes.
	head, err := peekFile(plain, 512)
	if err != nil {
		return channel.MediaRef{}, fmt.Errorf("wecom: media sniff: %w", err)
	}
	filename, contentType := describeMedia(wm, index, m, headers.Filename, head)

	if _, err := streamer.UploadStream(ctx, key, plain, size, contentType, filename); err != nil {
		return channel.MediaRef{}, fmt.Errorf("upload media: %w", err)
	}
	return channel.MediaRef{
		Type:       m.Kind,
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  size,
	}, nil
}

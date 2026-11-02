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
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// The two failure lines are copyPack.MediaUnreadable / MediaTooLarge
// (strings.go), read in the language of the chat they land in.

// mediaNoticeLocaleTimeout bounds the profile lookup behind the failure
// notice. Deliberately small: the notice is worth sending in the wrong
// language, and not worth delaying.
const mediaNoticeLocaleTimeout = 2 * time.Second

// mediaNoticeTimeout bounds the notice ResolveMedia sends about its own
// failures: the lookup above, a wait for the chat's turn behind an answer
// already streaming, and one server acknowledgement. Same size, and same
// reasoning, as the Router's bound on the notice it asks for when its budget
// runs out (engine.mediaAbandonNoticeTimeout).
const mediaNoticeTimeout = 15 * time.Second

// mediaNoticeQueries is what the failure notice reads before it writes: the
// reader's language, and whether the installation may still write at all
// (replyWriteGuard). *db.Queries is both.
type mediaNoticeQueries interface {
	languageLookup
	installationReader
}

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
	// queries picks the failure notice's language from where it lands
	// (language.go: a 1:1 reads the sender's profile, a room the
	// deployment's) and asks, as the notice is written, whether the
	// installation was revoked since the message came in. nil puts every
	// notice on the deployment default and writes it unchecked.
	queries mediaNoticeQueries
	// notices counts the failure notices ResolveMedia has sent off on their
	// own goroutines, so that the Router's Drain can wait for them
	// (DrainMediaNotices) the way it waited when they were sent inline.
	notices sync.WaitGroup
	logger  *slog.Logger
}

// NewMediaResolver builds the wecom MediaResolver. storage and ledger are
// required — without either there is nothing durable to point an attachment
// at, and the resolver degrades to leaving the placeholder in place. senders
// is optional: without it a failed attachment is only logged and the failure
// counter — which reports through the same registry — goes nowhere; so is
// queries, without which the notice is written in the deployment's language
// and is not checked against a revoked installation.
func NewMediaResolver(storage mediaStorage, ledger engine.MediaIntentLedger, senders *sendersRegistry, queries mediaNoticeQueries, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	if queries == nil {
		logger.Warn("wecom media: no queries wired; attachment-failure notices go out in the deployment language whatever the reader's profile says, and are written even after the installation is revoked")
	}
	return &wecomMediaResolver{
		storage: storage,
		ledger:  ledger,
		// Never a bare http.Client: the URL being fetched came off the wire,
		// and this client refuses to connect to anything that is not public
		// internet (media_guard.go).
		http:    newMediaHTTPClient(mediaGuard{}),
		notify:  senders,
		queries: queries,
		logger:  logger,
	}
}

// recordFailure reports n attachments that never reached the agent, under
// the reason an operator would act on (the mediaDrop* constants).
//
// It goes through the senders registry the resolver already holds rather than
// a sink of its own: this type is built at boot from router.go with five
// arguments already, and the registry is the one thing here that both knows
// about metrics and is guaranteed to be the same instance the read loop
// reports through.
//
// Each attachment is counted at exactly one of two places. ResolveMedia counts
// every attachment it failed on, whatever the reason. NotifyMediaAbandoned
// counts the ones ResolveMedia never saw fail: all of them when it never ran,
// and the refs it returned that the Router then dropped when it did.
func (r *wecomMediaResolver) recordFailure(reason string, n int) {
	if r.notify == nil {
		return
	}
	for range n {
		r.notify.RecordMediaFailure(reason)
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
			// Counted per attachment, not per message: appendFailure
			// deliberately collapses four expired links into one sentence for
			// the sender, and an operator asking how much media is being lost
			// wants the four. Counted here rather than beside the notice for
			// the same reason the log line is here — tellTheSender is skipped
			// entirely when there is no live socket to write to, which is
			// exactly the moment a failure most wants recording.
			if failure == mediaFailureUnreadable && ctx.Err() != nil {
				// The Router's budget cut this one off: it ran out mid-download,
				// or before this attachment's turn came and every step on the
				// dead context failed at once. Counted as that, not as
				// unreadable, or a budget too small for the files people send
				// would read as WeCom serving broken links. Left out of the
				// sender's notice: the Router is about to ask for that one
				// (NotifyMediaAbandoned).
				r.recordFailure(abandonReason(ctx.Err()), 1)
			} else {
				r.recordFailure(failure.metricReason(), 1)
				failures = appendFailure(failures, failure)
			}
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

	// When the Router's media budget ran out under us, the Router follows with
	// NotifyMediaAbandoned — and that says the same sentence, because from
	// where the sender is sitting it is the same event. Saying it here as well
	// gives them the identical apology twice in a row, which reads as the bot
	// glitching rather than as one attachment that did not make it.
	//
	// Only the line the Router is about to repeat is dropped. A "too big"
	// verdict was reached on its own merits before the clock ran out, and it
	// is the one the sender can actually act on, so it still goes.
	if ctx.Err() != nil {
		failures = withoutNotice(failures, mediaFailureUnreadable)
	}

	// On its own goroutine and its own deadline: the Router binds this
	// message, clears its pending-media marker and promotes its task only once
	// this returns, and a notice waiting on the chat's turn or on the server's
	// acknowledgement must not hold that up, nor spend the budget that decides
	// whether the refs above are kept. Counted in notices, so the Router's
	// Drain still waits for it (DrainMediaNotices).
	if len(failures) > 0 {
		r.notices.Add(1)
		go func() {
			defer r.notices.Done()
			noticeCtx, cancel := context.WithTimeout(context.Background(), mediaNoticeTimeout)
			defer cancel()
			r.tellTheSender(noticeCtx, inst, wm, failures)
		}()
	}
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
		// decrypt, the plaintext lands in a private temp file, and the
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
	return m.inline(channel.MediaRef{
		Type:       m.Kind,
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  int64(len(plain)),
	}), nil
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

// abandonReason is the label for an attachment the Router's budget took, by
// what ended the budget: running out, or the server shutting down. The two
// want different answers (a bigger budget or a faster media host, versus
// nothing), and a deploy would otherwise read as a slow-download incident.
func abandonReason(cause error) string {
	if errors.Is(cause, context.Canceled) {
		return mediaDropShutdown
	}
	return mediaDropBudgetExpired
}

// metricReason is the label this failure is counted under. The sender's
// wording folds "blocked" into "unreadable" — there is nothing they can do
// differently either way — but the operator's does not, which is the whole
// reason this kind exists.
func (f mediaFailure) metricReason() string {
	switch f {
	case mediaFailureTooLarge:
		return mediaDropTooLarge
	case mediaFailureBlocked:
		return mediaDropBlockedAddress
	default:
		return mediaDropUnreadable
	}
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

// noticeKind collapses the failure kinds that read as the same sentence down
// to the one that names it. Two kinds do: mediaFailureBlocked lands on the
// unreadable wording deliberately, because from the sender's side a refused
// address and a download that fell over are the same event — the attachment
// did not arrive — and the thing that separates them is what the operator must
// change, which belongs in the log. Neither the resolved address nor the
// signed url goes into a chat message.
//
// Anything reasoning about "would these two read the same" asks here. It
// cannot compare the rendered sentences: since the copy pack those are the
// reader's, and two readers in one group get different strings for the one
// event.
func noticeKind(f mediaFailure) mediaFailure {
	if f == mediaFailureTooLarge {
		return mediaFailureTooLarge
	}
	return mediaFailureUnreadable
}

// noticeFor is the one place a failure kind becomes a sentence, in the
// language c is written in.
func noticeFor(c copyPack, f mediaFailure) string {
	if noticeKind(f) == mediaFailureTooLarge {
		return c.MediaTooLarge
	}
	return c.MediaUnreadable
}

// withoutNotice drops every kind that would read the same as drop, leaving the
// rest in order. It goes through noticeKind rather than comparing kinds
// directly because the sender reads sentences: dropping mediaFailureUnreadable
// while leaving mediaFailureBlocked in the list would still print the same
// line twice.
func withoutNotice(list []mediaFailure, drop mediaFailure) []mediaFailure {
	out := list[:0]
	for _, f := range list {
		if noticeKind(f) != noticeKind(drop) {
			out = append(out, f)
		}
	}
	return out
}

// DrainMediaNotices answers engine.MediaNoticeDrainer: it returns once every
// failure notice ResolveMedia has sent off has finished. Each runs under
// mediaNoticeTimeout, so the wait is bounded by that.
func (r *wecomMediaResolver) DrainMediaNotices() {
	r.notices.Wait()
}

// NotifyMediaAbandoned answers engine.MediaAbandonNotifier: the Router's own
// media budget expired, so this resolver either never ran or was cut off, and
// the Router has dropped whatever refs it did return.
//
// It counts what ResolveMedia could not: every attachment when ResolveMedia
// never ran, and otherwise only the refs it returned that the Router dropped.
// The ones ResolveMedia failed on it has already counted, so counting them
// here would count them twice.
//
// The sender gets the same line as an attachment that could not be read,
// because from where they are sitting it is the same event and the same
// remedy: the picture did not get through, send it again. Which side of the
// seam ran out of time is ours to know and theirs not to care about.
//
// ctx is the Router's, with its own deadline; the lookup and the write below
// both run under it.
func (r *wecomMediaResolver) NotifyMediaAbandoned(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, a engine.MediaAbandonment) {
	wm, err := wecomMsgFromRaw(msg)
	if err != nil || len(wm.Media) == 0 {
		return
	}
	lost := len(a.Dropped)
	if !a.Started {
		lost = len(wm.Media)
	}
	r.recordFailure(abandonReason(a.Cause), lost)
	r.tellTheSender(ctx, inst, wm, []mediaFailure{mediaFailureUnreadable})
}

// tellTheSender writes one short notice into the chat the attachments came
// from, over the same live aibot socket every other wecom message uses.
//
// Without it the failure is invisible in the worst way: the stored body still
// says [Image], so the agent answers as if it had been shown a picture it
// never received. The agent run itself is deliberately left to proceed — the
// person can see for themselves that the picture did not get through, and the
// answer to whatever they typed beside it is still worth having.
func (r *wecomMediaResolver) tellTheSender(ctx context.Context, inst engine.ResolvedInstallation, wm InboundMessage, failures []mediaFailure) {
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
	// ctx is the notice's own, never the media budget's: the commonest reason
	// to be here is a download that ran out of time, and on that path the
	// budget is already spent. The lookup gets a shorter slice of it, so a
	// stalled profile read costs the notice its language and not the notice.
	lookupCtx, cancel := context.WithTimeout(ctx, mediaNoticeLocaleTimeout)
	c := copyFor(localeFor(lookupCtx, r.queries, inst.ID, chatType, wm.SenderUserID))
	cancel()

	lines := make([]string, 0, len(failures))
	for _, f := range failures {
		// Deduped by sentence, not by kind: two kinds share the unreadable
		// wording (see noticeFor), so a message with one blocked and one
		// unreadable attachment is still one piece of news. That is why the
		// dedupe is here rather than in appendFailure, which the operator log
		// needs kept per-kind.
		if notice := noticeFor(c, f); !slices.Contains(lines, notice) {
			lines = append(lines, notice)
		}
	}
	// guardReply: the installation was read when the message came in, which
	// can be a whole media budget ago. A revocation since then stops the
	// notice at the socket, as it stops every reply frame.
	if err := sender.sendTextCtx(guardReply(ctx, r.queries, inst.ID), chatID, chatType, strings.Join(lines, "\n")); err != nil {
		r.logger.Warn("wecom media failure notice not delivered",
			"installation_id", util.UUIDToString(inst.ID), "msg_id", wm.MsgID, "err", err)
	}
}

// errStreamingUnavailable says the memory-flat path could not be taken for a
// reason that is not the attachment's fault, such as no temp space.
// The caller falls back to the buffered path rather than failing an
// attachment over an optimisation.
var errStreamingUnavailable = errors.New("wecom: streaming media ingest unavailable")

// ingestStreaming carries one attachment without ever holding it whole:
// ciphertext streams from the response body into the decrypt, the plaintext
// lands in a private temp file, and the upload reads from that file with
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
		// back.
		if errors.Is(err, errMediaTempFile) {
			return channel.MediaRef{}, fmt.Errorf("%w: %w", errStreamingUnavailable, err)
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
	return m.inline(channel.MediaRef{
		Type:       m.Kind,
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  size,
	}), nil
}

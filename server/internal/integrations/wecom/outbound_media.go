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
	"errors"
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

// What the user is told, and there are three of these because there are three
// things that can be true — telling them apart is the point (see deliveryState).
// Hardcoded Chinese, like every other user-facing string this adapter sends
// (replier.go) — WeCom deployments are China-only.
const (
	// mediaSendFailedText — we know it did not arrive. Definite, because
	// claiming a definite failure that later turns out to be a delivery is how
	// a user ends up ignoring the notice.
	mediaSendFailedText = "⚠️ 有文件没能发出来，我这边保留着，需要的话我再试一次。"

	// mediaSendUnknownText — the frame went out and no verdict came back, so
	// the file may be in the chat already. The wording has to survive both
	// endings: it must not say "failed" to someone looking at the file, and it
	// must not say "sent" to someone who never got it. It also explains why
	// nothing is resent automatically, since that is the obvious next question
	// and the answer is that a duplicate cannot be taken back.
	mediaSendUnknownText = "⚠️ 有文件我没收到企业微信的送达回执，可能已经发到了、也可能没有。我不会自动重发，免得发重了；你那边没看到的话说一声，我再发一次。"

	// mediaLookupFailedText — the failure is on our side and before the
	// question was even answered: we could not read what was attached to this
	// reply, so we do not know whether there was a file. Saying nothing here is
	// what leaves a user waiting for something that was never attempted.
	mediaLookupFailedText = "⚠️ 我这边没查到这条回答带没带文件，所以要是有，这次没发出来。需要的话我再试一次。"
)

// attachmentBudget bounds one answer's whole attachment delivery — reading
// every object, uploading it, and sending it. Generous because a 20 MiB file
// over forty acked chunks, two at a time, is not fast, and nothing is waiting
// on it.
const attachmentBudget = 5 * time.Minute

// deliveryState is what we actually know about one file after trying to send
// it. Three values, because the two-valued version of this was wrong in both
// directions at once: a send whose ack never came was reported to the user as a
// definite failure even though the file may well be sitting in the chat, and
// the local failures that never reached the socket at all were reported to
// nobody.
type deliveryState int

const (
	// deliveryDelivered — WeCom acknowledged the send. The only state that
	// needs no message to the user; the file is what they see.
	deliveryDelivered deliveryState = iota

	// deliveryDefinitelyFailed — nothing arrived and nothing can have. Either
	// the file never became a media_id (the upload was refused, or the object
	// could not be read), or the send itself came back refused. Safe to retry
	// in principle, and safe to describe as a failure.
	deliveryDefinitelyFailed

	// deliveryUnknown — the send frame went out and no verdict came back. The
	// message may be in the chat. This state must never be retried: the same
	// media_id sent twice shows the person the file twice and there is nothing
	// to undo it with.
	deliveryUnknown
)

// String names the states for the log, in the vocabulary the code reasons in,
// so an operator reading a line can tell an unconfirmed send from a refused one
// without knowing which errcode meant which.
func (d deliveryState) String() string {
	switch d {
	case deliveryDelivered:
		return "delivered"
	case deliveryDefinitelyFailed:
		return "definitely_failed"
	case deliveryUnknown:
		return "unknown"
	default:
		return "invalid"
	}
}

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
	// SessionID is carried only so a delivery that fails minutes later can
	// still name the conversation it belonged to in the log and the counter.
	SessionID string
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
// carriesTheReply says the files ARE this reply's substance — the agent said
// nothing and bound a file instead. When it is true nothing has recorded a
// reply outcome yet and this path owes exactly one; when it is false the words
// already landed and the reply's outcome is settled, so only the per-file
// counters move here.
func (o *Outbound) deliverAttachments(e events.Event, to attachmentTarget, carriesTheReply bool) {
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
	// Admission is claimed here rather than inside the goroutine, because a
	// goroutine that has already started is a goroutine this cap did not
	// bound. The lookup it runs is on the far side of this gate too: under a
	// slow database, unbounded lookups are the same failure as unbounded
	// goroutines wearing a different hat.
	//
	// Nothing is known about this turn yet — whether a file is bound to it is
	// exactly what the lookup would tell us — so a refusal here is logged and
	// not spoken. Telling the user their file was dropped when the turn may
	// have carried none is the false alarm the post-lookup gate exists to
	// avoid.
	if !o.admitAttachmentDelivery() {
		// A scheduling refusal, counted under its own unit, and NOTHING else:
		// the comment above is literal — this gate runs before the lookup, so
		// it knows neither how many files the turn carries nor whether it
		// carries any at all. A per-file counter fed from here fabricates
		// cardinality, and a reply outcome asserted from here fabricates the
		// reply: an empty completion with zero bound files would be counted
		// as a dropped reply when the lookup would have classified it as
		// nothing_to_say. What cannot be known here is not recorded here.
		o.attachmentShed()
		o.logger.Warn("wecom outbound: attachment delivery not admitted, too many already running",
			"installation_id", uuidStringPub(to.InstallationID),
			"admitted", maxAdmittedAttachmentDeliveries)
		return
	}
	o.spawn(func() {
		defer o.releaseAttachmentAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), attachmentBudget)
		defer cancel()
		o.sendAttachments(ctx, messageID, workspaceID, to, carriesTheReply)
	})
}

// sendAttachments delivers every file bound to one answer. Files are
// independent: one that fails does not stop the rest, and what is known about
// the ones that did not plainly arrive is said once at the end rather than once
// each.
//
// The caller has already claimed admission, which is what bounds the number of
// goroutines running this and the number of lookups below. What is rationed
// here is different and deliberately after the lookup: a turn with no file
// bound to it must not consume a pending slot, and — the reason this matters
// to the user rather than to the scheduler — a delivery refused for want of
// one can only be reported honestly by something that already knows a file was
// waiting. Rationing this stage ahead of the lookup would either stay silent
// about a file that was dropped or warn about a file that never existed.
func (o *Outbound) sendAttachments(ctx context.Context, messageID, workspaceID pgtype.UUID, to attachmentTarget, carriesTheReply bool) {
	// replyFailed records this reply's single outcome, but only when the files
	// ARE the reply. When words already landed the reply is settled and only
	// the per-file counters move.
	replyFailed := func(reason dropReason, err error) {
		if carriesTheReply {
			o.droppedFor(ctx, to.SessionID, "chat:done", reason, err)
		}
	}
	rows, err := o.q.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
		ChatMessageID: messageID,
		WorkspaceID:   workspaceID,
	})
	if err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: attachment lookup failed",
			"error", err, "chat_message_id", uuidStringPub(messageID))
		o.tellUser(ctx, to, mediaLookupFailedText)
		replyFailed(dropTransport, err)
		return
	}
	if len(rows) == 0 {
		// mayCarryAttachments said there might be one; there was not.
		if carriesTheReply {
			o.skippedFor(ctx, to.SessionID, skipNothingToSay)
		}
		return
	}
	// Past here a file is known to be waiting, so every way out of this
	// function has to end in either a delivery or a sentence to the user.

	// Shed when too many deliveries that found a file are already outstanding.
	// The semaphore below bounds how many RUN at once; this bounds how many
	// wait for it, and unlike admission it can name what was dropped, so the
	// user hears about it.
	if !o.claimAttachmentSlot() {
		// Unlike admission, the rows are known here, so the accounting can be
		// exact: one drop per file that will not be sent, and — when the files
		// were the reply — the reply's own outcome.
		for range rows {
			o.attachmentDropped(ctx, dropAttachmentNotAdmitted, nil)
		}
		replyFailed(dropAttachmentNotAdmitted, nil)
		o.logger.WarnContext(ctx, "wecom outbound: attachment delivery shed, too many already pending",
			"installation_id", uuidStringPub(to.InstallationID),
			"attachments", len(rows),
			"pending", maxPendingAttachmentDeliveries)
		o.tellUser(ctx, to, mediaSendFailedText)
		return
	}
	defer o.releaseAttachmentSlot()

	// Acquired here and never before the spawn. Bus.Publish is synchronous on
	// the task-completion goroutine, so blocking out there would wedge the
	// completion path for up to the attachment budget — which is the very thing
	// the spawn exists to prevent.
	select {
	case attachmentSlots <- struct{}{}:
		defer func() { <-attachmentSlots }()
	case <-ctx.Done():
		// The rows are known here, so every one of them settles: nothing was
		// sent, which is the one case that is provably local.
		for range rows {
			o.attachmentDropped(ctx, dropTransport, ctx.Err())
		}
		o.logger.WarnContext(ctx, "wecom outbound: attachment delivery gave up waiting for a slot",
			"installation_id", uuidStringPub(to.InstallationID), "attachments", len(rows))
		// Deliberately on a fresh context: the one that expired is the reason
		// we are here, and reusing it would drop the sentence too.
		o.tellUser(context.WithoutCancel(ctx), to, mediaSendFailedText)
		replyFailed(dropTransport, ctx.Err())
		return
	}

	// Resolved here rather than carried in from the caller: the send that
	// delivered the words may have been minutes ago on a socket since replaced,
	// and the registry always holds the live one.
	sender := o.senders.get(to.InstallationID)
	if sender == nil {
		// Nothing to say it with — the socket that would carry the apology is
		// the socket that is missing. The log is the only place this can go.
		o.logger.WarnContext(ctx, "wecom outbound: no live connection for attachment delivery",
			"installation_id", uuidStringPub(to.InstallationID), "attachments", len(rows))
		for range rows {
			o.attachmentDropped(ctx, dropNoConnection, nil)
		}
		replyFailed(dropNoConnection, nil)
		return
	}
	failed, unknown := 0, 0
	// replyReason aggregates the DEFINITE per-file reasons for the reply's own
	// outcome, under worseDropReason's precedence — so a single refused file
	// surfaces as platform_refused on the reply too. Unknown outcomes stay out
	// of it: a file that may have arrived must not turn the reply into a
	// definite drop.
	var replyReason dropReason
	var replyUnconfirmed string
	for _, row := range rows {
		state, err := o.sendAttachment(ctx, sender, row, to)
		switch state {
		case deliveryDefinitelyFailed:
			failed++
			r := classifyDrop(err)
			o.attachmentDropped(ctx, r, err)
			replyReason = worseDropReason(replyReason, r)
		case deliveryUnknown:
			unknown++
			// Not a failure: the frame may well have arrived. Filed under its
			// own counter so the drop rate stays a rate of definite drops.
			r := unconfirmedReason(err)
			if r == "" {
				r = "ack_timeout" // sendOutcome and unconfirmedReason share a set; belt and braces
			}
			o.attachmentUnconfirmed(ctx, r, err)
			replyUnconfirmed = worseUnconfirmedReason(replyUnconfirmed, r)
		default:
			o.attachmentDelivered()
		}
		if err != nil {
			// The object's URL stays out of the log: it is an address that
			// serves the file to whoever holds it.
			o.logger.WarnContext(ctx, "wecom outbound: attachment not confirmed delivered",
				"error", err,
				"delivery", state.String(),
				"installation_id", uuidStringPub(to.InstallationID),
				"attachment_id", uuidStringPub(row.ID),
				"content_type", row.ContentType,
				"size_bytes", row.SizeBytes)
		}
	}
	if carriesTheReply {
		// The files were the answer. Three honest endings: any file arrived —
		// delivered; none arrived and at least one failure is definite — a
		// drop under the worst definite reason; nothing definite either way —
		// unconfirmed, because calling a maybe-delivered reply a drop points
		// the operator at a resend the user may already have.
		switch {
		case failed+unknown < len(rows):
			o.delivered()
		case failed > 0:
			o.droppedFor(ctx, to.SessionID, "chat:done", replyReason, nil)
		default:
			o.unconfirmedFor(ctx, to.SessionID, "chat:done", replyUnconfirmed, nil)
		}
	}
	// The answer is already on the user's screen and it may well refer to a
	// file. Saying nothing would leave them looking for one that never comes —
	// but saying "it failed" about a file that did arrive is its own harm, so
	// each group speaks for itself and an unconfirmed send never borrows the
	// definite wording.
	var lines []string
	if failed > 0 {
		lines = append(lines, mediaSendFailedText)
	}
	if unknown > 0 {
		lines = append(lines, mediaSendUnknownText)
	}
	if len(lines) > 0 {
		o.tellUser(ctx, to, strings.Join(lines, "\n"))
	}
}

// tellUser puts one sentence into the conversation, best effort. Every caller
// is already on a path where something went wrong, so a failure here is logged
// and dropped rather than propagated — there is nothing further to try.
func (o *Outbound) tellUser(ctx context.Context, to attachmentTarget, text string) {
	if o.senders == nil {
		return
	}
	sender := o.senders.get(to.InstallationID)
	if sender == nil {
		return
	}
	if err := sender.sendTextCtx(ctx, to.ChatID, to.ChatType, text); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: could not tell the user about the file",
			"error", err, "installation_id", uuidStringPub(to.InstallationID))
	}
}

// sendAttachment carries one file from object storage into the chat, and
// reports what is known about where it ended up. The error is for the log; the
// state is what the user is told.
func (o *Outbound) sendAttachment(ctx context.Context, sender *wsSender, row db.Attachment, to attachmentTarget) (deliveryState, error) {
	// The recorded size is checked before a single byte is fetched. An
	// oversize attachment is refused either way — readObject re-checks what it
	// actually read, because the column is metadata and the object is the
	// truth — but reading 40 MB out of storage to then refuse it is work
	// nobody benefits from.
	if row.SizeBytes > maxMediaUploadBytes {
		return deliveryDefinitelyFailed, fmt.Errorf("attachment is %d bytes: %w", row.SizeBytes, errMediaUploadTooLarge)
	}
	data, err := o.readObject(ctx, row.Url)
	if err != nil {
		return deliveryDefinitelyFailed, err
	}
	kind := wecomMediaKind(row.ContentType, row.Filename, len(data))
	name := outboundMediaName(row.Filename, row.ContentType)

	mediaID, err := sender.uploadMedia(ctx, outboundMedia{
		Kind:     kind,
		Filename: name,
		Data:     data,
	})
	if err != nil {
		// A failed upload never produced a media_id, so no message was ever
		// addressed to the chat — including when the failure was the finish
		// step's own lost ack. The file is definitely not there.
		return deliveryDefinitelyFailed, fmt.Errorf("upload %s: %w", kind, err)
	}
	// Video is the only kind with fields beyond the media_id, and both are
	// required. The file's own name is what there is to say about it — the
	// attachment row carries no caption and the agent's words are already in
	// the message above.
	err = sender.sendMedia(ctx, to.ChatID, to.ChatType, mediaSend{
		Kind:        kind,
		MediaID:     mediaID,
		Title:       strings.TrimSuffix(name, path.Ext(name)),
		Description: name,
	})
	return sendOutcome(err), err
}

// sendOutcome reads a media push's error for what it says about the message.
//
// The one distinction that matters: a refusal is WeCom answering, and a missing
// answer is not an answer. errAckTimeout means the frame reached the socket and
// the verdict never came, which leaves the message possibly delivered.
//
// A transport failure lands there too, and for the same reason rather than a
// weaker one. errWriteAttempted marks an error raised once WriteMessage had
// been entered; past that point the frame may already be at the peer, since a
// half-closed connection reports "broken pipe" to the writer for bytes the
// reader has. Only what fails before the write — a marshal error, a deadline
// the connection refused — is provably undelivered.
//
// A context error lands on the same side, and less precisely than one would
// like. wsSender.request returns the same ctx.Err() whether the context ended
// before the frame was written or while waiting for its verdict, so from out
// here the two cannot be told apart. Reading all of these as unknown is the
// direction that costs least: an unknown is never resent and is described in
// words that hold either way, so a send that never happened is under-claimed
// rather than a send that did happen being denied.
func sendOutcome(err error) deliveryState {
	switch {
	case err == nil:
		return deliveryDelivered
	case errors.Is(err, errAckTimeout),
		errors.Is(err, errWriteAttempted),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return deliveryUnknown
	default:
		return deliveryDefinitelyFailed
	}
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
	// that has more to come. The cap is the platform's, not the framing's, so
	// the read stops there rather than at the 50 MB the chunk protocol could
	// have expressed — the extra 30 MB would only ever be resident long enough
	// to be refused.
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

	// maxPendingAttachmentDeliveries bounds the deliveries that have found a
	// file and are waiting for a slot. Past it a delivery is shed and the
	// user is told: the answer's text has already reached them, the
	// attachment is still in object storage, and silence would leave them
	// waiting for a file that is not coming.
	maxPendingAttachmentDeliveries = 32

	// maxAdmittedAttachmentDeliveries bounds the goroutines themselves, and
	// with them the attachment lookups they run before anything about the
	// turn is known. Twice the pending cap as headroom, not as a derived
	// quantity: a turn holds admission for its whole life but claims a pending
	// slot only once its lookup has found a file, so a backlog of
	// file-carrying turns meets the pending cap first — the ordering that
	// keeps the user-facing shed on the path that can name a real file.
	//
	// So reaching THIS cap does not imply the pending one is full: turns still
	// inside their lookup hold admission and no pending slot, and turns that
	// find no file hold admission for their whole life and never claim one.
	// TestDeliverAttachments_AdmissionBoundsTheLookupStage is the first of
	// those — it parks every goroutine in the lookup and reaches the admitted
	// cap with pending at zero.
	maxAdmittedAttachmentDeliveries = 2 * maxPendingAttachmentDeliveries
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

// admitAttachmentDelivery reserves the right to start one delivery goroutine,
// or reports that too many are already running. Claimed before the spawn:
// after it, the goroutine and its lookup are already past anything this could
// bound.
func (o *Outbound) admitAttachmentDelivery() bool {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	if o.admittedAttachments >= maxAdmittedAttachmentDeliveries {
		return false
	}
	o.admittedAttachments++
	return true
}

func (o *Outbound) releaseAttachmentAdmission() {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	o.admittedAttachments--
}

package wecom

// reply_revocation_test.go — a reply whose installation is revoked, or whose
// permission cannot be read, while the reply waits for its turn on the socket.
//
// The rule is attachment_revocation_test.go's, applied to words: once the
// permission is gone, nothing more of the reply is written — not the message,
// not the closing frame of its bubble, not the notice a failed run leaves.
// The address was read before the chat's turn and the writer's; each test
// holds the writer, changes the permission while the frame waits for it, lets
// it go, and counts what reached the socket.
//
// What separates the two ways of losing it is the outcome. Revoked is a reply
// nobody is owed (skipInstallationInactive). Unreadable is a lookup that failed
// ahead of the write: a drop on the direct path, another offer on the relay.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// replyRevocationRig is one chat on one installation, with a turn asked over
// WeCom and no bubble: the answer goes out as a plain message.
type replyRevocationRig struct {
	o      *Outbound
	q      *revocableQueries
	conn   *mediaConn
	sender *wsSender
	mx     *countingMetrics
}

func newReplyRevocationRig(t *testing.T) *replyRevocationRig {
	t.Helper()
	id := mustTestUUID(t)
	inner := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{InstallationID: id, ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{ID: id, Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	inner.fileTask(t, testTaskID)
	r := &replyRevocationRig{
		q:    &revocableQueries{fakeOutboundQueries: inner, id: id},
		conn: newMediaConn(),
		mx:   newCountingMetrics(),
	}
	r.sender = r.conn.newSender()
	reg := newSendersRegistry()
	reg.set(id, r.sender)
	r.o = NewOutbound(r.q, reg, nil, testLogger(), WithOutboundMetrics(r.mx))
	r.o.spawn = func(f func()) { f() }
	return r
}

// whileTheWriterIsHeld runs send with the socket's writer taken, lets it reach
// the wait, applies change, and frees the writer. It returns once send has.
func whileTheWriterIsHeld(t *testing.T, sender *wsSender, send func(), change func()) {
	t.Helper()
	if err := sender.lockWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		send()
	}()
	settle() // the frame is now parked on the writer
	change()
	sender.unlockWriter()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the send never returned")
	}
}

// The answer was addressed while the installation was live, and waits for the
// writer while it is revoked. It must not go, and it is not a failed delivery:
// the same skip the address lookup files when the revocation came first.
//
// REVERSE VERIFICATION: drop guardReply from sendAsMessage's sendTextCtx and
// this fails with the answer on the wire and counted delivered.
func TestAnAnswerQueuedForTheWriterIsNotSentAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	var err error
	whileTheWriterIsHeld(t, r.sender,
		func() { err = r.o.processEvent(context.Background(), chatDoneEvent("the answer")) },
		func() { r.q.revoked.Store(true) })

	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Fatalf("%d send frames, want 0: the answer went out after the revoke", n)
	}
	if got := r.mx.get("outbound_skipped:" + string(skipInstallationInactive)); got != 1 {
		t.Errorf("outbound_skipped:installation_inactive = %d, want 1", got)
	}
	if got := r.mx.get("outbound_delivered") + r.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("delivered+dropped = %d, want 0: a revoked installation is not owed the reply", got)
	}
}

// The same wait, but the permission read fails instead. Nothing goes either,
// and this time it is a drop: the reply was owed and was not delivered.
func TestAnAnswerWhosePermissionCannotBeReadIsNotSent(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	var err error
	whileTheWriterIsHeld(t, r.sender,
		func() { err = r.o.processEvent(context.Background(), chatDoneEvent("the answer")) },
		func() { r.q.unreadable.Store(true) })

	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Fatalf("%d send frames, want 0", n)
	}
	if got := r.mx.get("outbound_dropped:" + string(dropTransport)); got != 1 {
		t.Errorf("outbound_dropped:transport_error = %d, want 1", got)
	}
	if got := r.mx.get("outbound_skipped"); got != 0 {
		t.Errorf("outbound_skipped = %d, want 0: an unreadable permission is not a revocation", got)
	}
}

// A long answer goes out in pieces, and the guard asks before each. Revoked
// after the first piece, the rest stays unsent; the first is on the reader's
// screen, so the reply counts as delivered — the partial-send rule.
func TestALongAnswerRevokedAfterItsFirstPieceStopsThere(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	r.conn.onWrite = func(cmd string) {
		if cmd == cmdSendMsg {
			r.q.revoked.Store(true)
		}
	}
	long := longAnswerOfPieces(t, 3)

	if err := r.o.processEvent(context.Background(), chatDoneEvent(long)); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 1 {
		t.Fatalf("%d send frames, want 1: pieces kept going after the revoke", n)
	}
	if got := r.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1: the first piece is on screen", got)
	}
	if got := r.mx.get("outbound_skipped"); got != 0 {
		t.Errorf("outbound_skipped = %d, want 0: part of the answer did land", got)
	}
}

// longAnswerOfPieces builds an answer splitForWire cuts into n pieces.
func longAnswerOfPieces(t *testing.T, n int) string {
	t.Helper()
	line := "a line of the answer that is long enough to add up\n"
	text := ""
	for len(splitForWire(text)) < n {
		text += line
	}
	return text
}

// A reply routed from another replica waits for the writer on the one that
// holds the socket, and is revoked there. The frame is finished — no replica
// may write it, so another offer would only walk the retry chain — and it is
// recorded as the skip, not as a drop.
//
// REVERSE VERIFICATION: drop guardReply from deliverRelayed's sendTextCtx and
// this fails with the reply on the wire.
func TestARelayedReplyQueuedForTheWriterIsNotSentAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	var res relayResult
	whileTheWriterIsHeld(t, r.sender,
		func() { res = r.o.deliverRelayed(context.Background(), r.relayedReply()) },
		func() { r.q.revoked.Store(true) })

	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Fatalf("%d send frames, want 0: the relayed reply went out after the revoke", n)
	}
	if res.outcome != outcomeDone {
		t.Fatalf("outcome = %v, want outcomeDone: a revoked installation will not take the frame on any replica", res.outcome)
	}
	if res.record == nil {
		t.Fatal("no record: the skip would go uncounted")
	}
	res.record()
	if got := r.mx.get("outbound_skipped:" + string(skipInstallationInactive)); got != 1 {
		t.Errorf("outbound_skipped:installation_inactive = %d, want 1", got)
	}
	if got := r.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0", got)
	}
}

// Unreadable on the relay is one more offer: the reply is still owed, and still
// the only copy.
func TestARelayedReplyWhosePermissionCannotBeReadIsOfferedAgain(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	var res relayResult
	whileTheWriterIsHeld(t, r.sender,
		func() { res = r.o.deliverRelayed(context.Background(), r.relayedReply()) },
		func() { r.q.unreadable.Store(true) })

	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Fatalf("%d send frames, want 0", n)
	}
	if res.outcome != outcomeProvablyNotSent {
		t.Fatalf("outcome = %v, want outcomeProvablyNotSent", res.outcome)
	}
}

func (r *replyRevocationRig) relayedReply() relayFrame {
	return relayFrame{
		Kind:           relayKindReply,
		InstallationID: util.UUIDToString(r.q.id),
		ChatID:         "CHAT_1",
		ChatType:       chatTypeGroupInt,
		Content:        "the answer",
		TaskID:         testTaskID,
		SessionID:      testSessionID,
	}
}

// An inbox push to a member's bot waits for the writer and the bot is revoked.
// Nothing goes, and it is a proven miss: the caller may offer the push to the
// member's other bot, which the binding lookup only returns when it is active.
//
// REVERSE VERIFICATION: drop guardReply from pushInbox and this fails with the
// push on the wire.
func TestAnInboxPushQueuedForTheWriterIsNotSentAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newReplyRevocationRig(t)
	binding := db.ChannelUserBinding{InstallationID: r.q.id, ChannelUserID: "USER_1"}
	var delivered, provenMiss bool
	whileTheWriterIsHeld(t, r.sender,
		func() {
			delivered, provenMiss = r.o.pushInbox(context.Background(),
				map[string]any{"id": "ITEM_1", "type": "mention"}, binding, "RECIPIENT", "a notification", false)
		},
		func() { r.q.revoked.Store(true) })

	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Fatalf("%d send frames, want 0: the push went out after the revoke", n)
	}
	if delivered || !provenMiss {
		t.Fatalf("delivered=%v provenMiss=%v, want false/true: nothing left the process", delivered, provenMiss)
	}
}

// revocableBubbleRig is a bubbleRig whose installation can be revoked or made
// unreadable mid-flight, on both halves: the outbound subscriber that closes a
// bubble with the answer and the typing manager that closes it with a notice.
type revocableBubbleRig struct {
	*bubbleRig
	rq *revocableQueries
	mx *countingMetrics
}

func newRevocableBubbleRig(t *testing.T) *revocableBubbleRig {
	t.Helper()
	rig := newBubbleRig(t)
	rq := &revocableQueries{fakeOutboundQueries: rig.q, id: rig.instID}
	mx := newCountingMetrics()
	rig.out = NewOutbound(rq, rig.senders, rig.streams, nil, WithOutboundMetrics(mx))
	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders: rig.senders, Streams: rig.streams, Tasks: rig.q, Deliveries: rq,
	})
	rig.bus = events.New()
	rig.typing.Register(rig.bus)
	return &revocableBubbleRig{bubbleRig: rig, rq: rq, mx: mx}
}

func (r *revocableBubbleRig) closingFrames(t *testing.T) int {
	t.Helper()
	n := 0
	for _, f := range r.conn.streamFrames(t) {
		if f["finish"] == true {
			n++
		}
	}
	return n
}

func (r *revocableBubbleRig) answerEvent(t *testing.T, taskName, content string) events.Event {
	return events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, taskName),
		Payload:       protocol.ChatDonePayload{Content: content},
	}
}

// The bubble was painted while the installation was live. The answer's closing
// frame waits for the writer and the installation is revoked: no closing
// frame, and no plain message in its place either.
//
// REVERSE VERIFICATION: remove the guard from writeStreamFrame and this fails
// with the answer in the bubble.
func TestABubblesClosingFrameIsNotWrittenAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newRevocableBubbleRig(t)
	r.ran(t, "REQ-1", "task-1")
	var err error
	whileTheWriterIsHeld(t, r.senders.get(r.instID),
		func() { err = r.out.processEvent(context.Background(), r.answerEvent(t, "task-1", "the answer")) },
		func() { r.rq.revoked.Store(true) })

	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := r.closingFrames(t); n != 0 {
		t.Fatalf("%d closing frames, want 0: the bubble was closed with the answer after the revoke", n)
	}
	if n := len(r.conn.pushes(t)); n != 0 {
		t.Fatalf("%d plain messages, want 0: the fallback went out after the revoke", n)
	}
	if got := r.mx.get("outbound_skipped:" + string(skipInstallationInactive)); got != 1 {
		t.Errorf("outbound_skipped:installation_inactive = %d, want 1", got)
	}
}

// A failed run's notice closes the bubble from the typing manager. Revoked
// while the notice waits: neither the closing frame nor the fallback message.
//
// REVERSE VERIFICATION: drop guardReply from writeClosing and this fails with
// the notice in the bubble.
func TestAFailedRunsNoticeIsNotWrittenAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newRevocableBubbleRig(t)
	r.ran(t, "REQ-1", "task-1")
	whileTheWriterIsHeld(t, r.senders.get(r.instID),
		func() { r.failed(t, "task-1", false) },
		func() { r.rq.revoked.Store(true) })

	if n := r.closingFrames(t); n != 0 {
		t.Fatalf("%d closing frames, want 0: the failure notice closed the bubble after the revoke", n)
	}
	if n := len(r.conn.pushes(t)); n != 0 {
		t.Fatalf("%d plain messages, want 0", n)
	}
}

// A run's ending said as a plain message — the round has no bubble — waits for
// the writer and is revoked.
//
// REVERSE VERIFICATION: drop guardReply from sayAsPlainMessage and this fails
// with the ending on the wire.
func TestARunsEndingSaidAsAMessageIsNotSentAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newRevocableBubbleRig(t)
	addr := roundAddress{InstallationID: r.instID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt}
	var err error
	whileTheWriterIsHeld(t, r.senders.get(r.instID),
		func() {
			err = r.typing.sayAsPlainMessage(context.Background(), bubbleSessionID(t), addr, taskUUID(t, "task-1"), "the run failed")
		},
		func() { r.rq.revoked.Store(true) })

	if !errors.Is(err, errInstallationGone) {
		t.Fatalf("err = %v, want errInstallationGone", err)
	}
	if n := len(r.conn.pushes(t)); n != 0 {
		t.Fatalf("%d plain messages, want 0", n)
	}
}

// blippingQueries answers the installation as active, except for the reads
// the test arms to fail — a database that drops one query and answers the next.
type blippingQueries struct {
	*fakeOutboundQueries
	id       pgtype.UUID
	failures atomic.Int32
}

func (b *blippingQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if b.failures.Add(-1) >= 0 {
		return db.ChannelInstallation{}, errors.New("wecom test: one query dropped")
	}
	if arg.ID != b.id {
		return db.ChannelInstallation{}, errors.New("wecom test: asked about another installation")
	}
	return db.ChannelInstallation{ID: b.id, Status: string(InstallationActive)}, nil
}

// The closing frame goes out and its ack is lost. Before the retry, one
// permission read fails, so the retry is refused unwritten. That refusal says
// nothing about the first frame, which may be on screen: the ending is
// unknown, and the answer must not follow as a plain message — the next read
// succeeds, so nothing else would stop it.
//
// REVERSE VERIFICATION: take the retry's error in streamStore.seal whatever it
// is, and this fails with the answer sent a second time as a message.
func TestARetryRefusedUnwrittenLeavesTheClosingFrameUnknown(t *testing.T) {
	t.Parallel()
	rig, mx := retryRig(t)
	bq := &blippingQueries{fakeOutboundQueries: rig.q, id: rig.instID}
	rig.out = NewOutbound(bq, rig.senders, rig.streams, nil, WithOutboundMetrics(mx))
	rig.ran(t, "REQ-BLIP", "task-1")
	rig.conn.loseClosingAcks = 1
	rig.conn.onClosing = func() { bq.failures.Store(1) }

	rig.answer(t, "the agent reply", "task-1")

	closing := 0
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			closing++
		}
	}
	if closing != 1 {
		t.Fatalf("%d closing frames, want 1: the refused retry wrote nothing", closing)
	}
	if n := len(rig.conn.pushes(t)); n != 0 {
		t.Fatalf("the answer also went out as %d plain message(s); the user may read it twice", n)
	}
	if got := mx.get("outbound_unconfirmed"); got != 1 {
		t.Errorf("outbound_unconfirmed = %d, want 1", got)
	}
}

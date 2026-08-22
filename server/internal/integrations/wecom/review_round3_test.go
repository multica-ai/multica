package wecom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Revoking an installation is a person withdrawing permission, and the socket
// that person's runs were answered over is not always reaped by the time the
// next answer is ready.
//
// Two ways in, and the second is the one that costs something irreversible.
// The bubble's closing frame is written through the registered socket without
// ever loading the installation — sendAsMessage's own status check guards only
// the plain-message fallback. And a file behind it is worse: a duplicate line
// of copy is scrolled past, a file sent into a chat whose owner said no cannot
// be taken back.
func TestARevokedInstallationIsWrittenToByNothing(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-REVOKED", 1, "task-1")
	if n := len(rig.conn.streamFrames(t)); n != 1 {
		t.Fatalf("the question opened %d bubbles, want 1 — this test's premise is gone", n)
	}

	// Permission is withdrawn while the run is in flight. The socket is still
	// registered: the reaper has not got to it.
	rig.q.revoke()

	rig.answer(t, "the answer", "task-1")

	if n := len(rig.conn.streamFrames(t)); n != 1 {
		t.Errorf("a closing frame went out on a revoked installation — the bubble path never loads the row, so nothing else was going to stop it")
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Errorf("wrote %q to a revoked installation", got)
	}
}

// The revocation can land between the attempt that booked a retry and the
// attempt that runs, which is up to fifteen minutes later. sayTheAnswer is the
// retry's own entry point, so asking there rather than in processEvent is what
// makes a booked attempt re-check instead of inheriting a decision taken before
// the withdrawal.
func TestARetryAsksAgainRatherThanInheritingPermission(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-LATE", 1, "task-1")
	opened := len(rig.conn.streamFrames(t))

	rig.q.revoke()

	// Straight at the entry point a booked attempt uses, bypassing everything
	// processEvent asks on the first pass.
	if err := rig.out.sayTheAnswer(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-1"),
		Payload:       protocol.ChatDonePayload{Content: "late"},
	}, bubbleSessionID(t), taskUUID(t, "task-1"), "late",
		attachmentTarget{InstallationID: rig.instID, ChatID: "CHAT_1", ChatType: 1},
		endingRetries{}.begin(time.Now())); err != nil {
		t.Fatalf("sayTheAnswer: %v", err)
	}

	if n := len(rig.conn.streamFrames(t)); n != opened {
		t.Errorf("a booked attempt wrote a closing frame after the installation was revoked")
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Errorf("a booked attempt wrote %q after the installation was revoked", got)
	}
}

// A writer-wait timeout is a failure ahead of the write, and it has to say so
// or the round gives up a bubble nothing touched.
//
// lockWriter is one level below the three returns respondStream names for
// itself: a caller that gives up waiting for the writer never reaches
// writeLocked at all, so writeLocked cannot be the one to say it. Left bare it
// read as a failure that may have painted the screen, and the retry that
// follows arrives beside a spinner that survives to the sweep.
func TestGivingUpOnTheWriterKeepsTheBubble(t *testing.T) {
	t.Parallel()
	s := newWSSender(&silentConn{}, nil)

	// Hold the writer, so the next caller can only wait.
	if err := s.lockWriter(context.Background()); err != nil {
		t.Fatalf("taking the writer: %v", err)
	}
	defer s.unlockWriter()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.lockWriter(ctx)
	if err == nil {
		t.Fatal("the second caller took a writer somebody else is holding")
	}
	if !errors.Is(err, errFrameNotOnTheWire) {
		t.Errorf("err = %v, want it to carry errFrameNotOnTheWire — not one byte reached the socket", err)
	}
	if !bubbleSurvivedTheFailure(err) {
		t.Errorf("bubbleSurvivedTheFailure(%v) = false — the round gives up a spinner nothing touched, and the retry speaks beside it", err)
	}
}

// A round somebody else already ended is not this call's to send a file behind.
//
// sayEnding answers roundToldAlready without ever running the send closure, and
// it answers it with a nil error. Read as success, that sends the attachment
// addressed from the binding — so a repeated chat:done puts the text on screen
// once and the file twice. Driven against a REAL ledger, because a store-less
// rig has no memory of having been told and cannot produce the verdict at all.
func TestAnEndingSaidByAnotherPublisherSendsNoFile(t *testing.T) {
	t.Parallel()
	streams := newStreamStore()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := newMediaConn()
	reg.set(instID, conn.newSender())

	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "a.txt", Url: "u"})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	o := NewOutbound(q, reg, streams, nil, WithAttachments(&fakeObjectStore{key: "u", data: []byte("X")}))
	o.spawn = func(f func()) { f() }

	e := chatDoneEvent("the answer")
	if err := o.processEvent(context.Background(), e); err != nil {
		t.Fatalf("first processEvent: %v", err)
	}
	first := len(mediaSends(t, conn))
	if first != 1 {
		t.Fatalf("the first delivery sent %d file(s), want 1 — this test's premise is gone", first)
	}

	// The same chat:done again. The ledger has been told this round's ending.
	if err := o.processEvent(context.Background(), e); err != nil {
		t.Fatalf("second processEvent: %v", err)
	}
	if got := len(mediaSends(t, conn)); got != first {
		t.Errorf("a repeated chat:done sent %d file(s) in total, want %d — an attachment is not a line of copy, nothing takes it back", got, first)
	}
}

var _ = pgtype.UUID{}

// A permission read that failed is not permission that was withdrawn, and the
// difference is the answer.
//
// The gate that closed the revocation hole returned a bool, so a database blip
// on one attempt read the same as "revoked" — and sayTheAnswer returned nil,
// which reports a one-shot chat:done handled while this subscriber is still
// holding the only copy of the reply. The read failing establishes nothing, so
// the attempt is refused and the answer stays owed.
//
// Eve's reproduction, driven exactly: open the bubble, fail the FIRST
// installation read, leave every later one healthy, and give the chain 20ms.
func TestAPermissionReadThatFailedDoesNotEatTheAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.out.retryAfter = 20 * time.Millisecond
	rig.ran(t, "REQ-BLIP", 1, "task-1")
	opened := len(rig.conn.streamFrames(t))

	// The first read blips; the retry behind it finds the row exactly as it is.
	rig.q.installErrFor = 1

	rig.answer(t, "42", "task-1")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(rig.conn.streamFrames(t)) == opened {
		time.Sleep(time.Millisecond)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) == opened {
		t.Fatalf("the answer was never said: a lookup that failed for one attempt was read as permission withdrawn, and chat:done was reported handled with the reply still in hand")
	}
	last := frames[len(frames)-1]
	if last["content"] != "42" || last["finish"] != true {
		t.Errorf("the attempt behind the blip wrote %v, want the answer sealed into the bubble", last)
	}
}

// Same conflation, the other side of the delivery.
//
// Once the words are out, re-running the whole answer would say them twice, so
// an unreadable permission cannot book a retry here. What it must not do is
// pass silently: the file is withheld and the failure is surfaced, because
// nothing else speaks for an attachment that never went.
func TestAPermissionReadThatFailedAfterTheAnswerSurfacesTheWithheldFile(t *testing.T) {
	t.Parallel()
	streams := newStreamStore()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := newMediaConn()
	reg.set(instID, conn.newSender())

	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "a.txt", Url: "u"})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	o := NewOutbound(q, reg, streams, nil, WithAttachments(&fakeObjectStore{key: "u", data: []byte("X")}))
	o.spawn = func(f func()) { f() }
	o.retryAfter = -1 // no retries: this is about the report, not the recovery

	// Reads: 1 the answer's own gate, 2 sendAsMessage's, 3 the post-answer
	// re-check. Only the third blips.
	q.installErrFor = 3

	err := o.processEvent(context.Background(), chatDoneEvent("the answer"))
	if err == nil {
		t.Error("processEvent reported success while the file was silently dropped — nothing else speaks for an attachment that never went")
	}
	if got := markdownSends(t, conn); len(got) != 1 {
		t.Errorf("text sends = %d, want 1 — the words go out either way", len(got))
	}
	if got := mediaSends(t, conn); len(got) != 0 {
		t.Errorf("sent %d file(s) on a permission nobody could read", len(got))
	}
}

package wecom

// stream_close_retry_test.go — what a closing frame does when its ack never
// comes, and what a delivery that failed after taking the bubble leaves behind.
//
// Measured against the live bot (STRATEGY §6.5): a closing frame written right
// before a disconnect does NOT land, and re-sending the identical closing frame
// on the same stream is accepted with errcode 0 whether or not the first one
// did. So the answer to a lost ack is the same frame again, on whatever socket
// the installation holds by then — not a plain message the user may then read
// twice. streamStore.seal is where that policy lives, and these hold it shut.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// retryRig is a bubbleRig whose closing frames give up on an ack quickly and
// retry without the two-second gap, so the retry policy runs in milliseconds.
func retryRig(t *testing.T) (*bubbleRig, *countingMetrics) {
	t.Helper()
	rig := newBubbleRig(t)
	mx := newCountingMetrics()
	rig.senders.WithMetrics(mx)
	// The same sink on both halves: recordEnding counts on the registry, the
	// delivered/unconfirmed/dropped verdict counts on Outbound, and a test that
	// wires only one of them reads a zero as "did not happen".
	rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil, WithOutboundMetrics(mx))
	rig.conn.sender.ackTimeout = 50 * time.Millisecond
	rig.streams.closeRetryDelay = 0
	return rig, mx
}

// The ack is lost once — the socket dropped right after the closing frame went
// out — and the Supervisor has reconnected by the time the retry is written.
// Exactly two identical closing frames reach the wire, one per socket, no
// plain message, and the ending is counted once as finished.
//
// REVERSE VERIFICATION: make seal return after its first attempt (replace the
// loop with a single senders.stream call) and this fails with one closing
// frame and the answer as a plain message on the new socket.
func TestAClosingFrameWhoseAckWasLostIsWrittenAgain(t *testing.T) {
	t.Parallel()
	rig, mx := retryRig(t)
	rig.ran(t, "REQ-RETRY", "task-1")
	dropped := rig.conn

	var next *bubbleConn
	dropped.loseClosingAcks = 1
	dropped.onClosing = func() {
		// The drop: the frame is on the wire, its ack never comes, and the
		// installation's socket is a new one by the time anybody retries.
		if next == nil {
			next = rig.reconnect()
			next.sender.ackTimeout = 50 * time.Millisecond
		}
	}

	rig.answer(t, "the agent reply", "task-1")

	first := dropped.streamFrames(t)
	if len(first) != 2 || first[1]["finish"] != true {
		t.Fatalf("the dropped socket carried %v, want the opener and one closing frame", first)
	}
	second := next.streamFrames(t)
	if len(second) != 1 {
		t.Fatalf("the new socket carried %d stream frames, want exactly the retried closing frame: %v", len(second), second)
	}
	for _, k := range []string{"id", "finish", "content"} {
		if first[1][k] != second[0][k] {
			t.Fatalf("the retry differs from the lost frame on %s: %v vs %v — a retry that is not the identical frame is a new message, not a repeat", k, first[1][k], second[0][k])
		}
	}
	if second[0]["content"] != "the agent reply" {
		t.Fatalf("the retried frame carries %q, want the answer", second[0]["content"])
	}
	if reqIDs := next.streamReqIDs(); len(reqIDs) != 1 || reqIDs[0] != "REQ-RETRY" {
		t.Fatalf("the retry echoed %v, want the callback's req_id", reqIDs)
	}
	if n := len(dropped.pushes(t)) + len(next.pushes(t)); n != 0 {
		t.Fatalf("the answer also went out as %d plain message(s); the user reads it twice", n)
	}
	if got := mx.get("stream_finished"); got != 1 {
		t.Errorf("stream_finished = %d, want 1 — one ending, however many attempts it took", got)
	}
	if got := mx.get("stream_fell_back"); got != 0 {
		t.Errorf("stream_fell_back = %d, want 0 — a bubble that took the retry did not fall back", got)
	}
}

// The ack never comes, on the same socket, attempt after attempt. The frame is
// written four times — the first and streamCloseRetries more — and then the
// delivery is left as unknown rather than repeated: four writes with no
// verdict is four chances the person is already reading it.
//
// REVERSE VERIFICATION: same line as above; this then fails with one closing
// frame instead of four.
func TestAClosingFrameNobodyAcksIsRetriedAndThenLeftUnknown(t *testing.T) {
	t.Parallel()
	rig, mx := retryRig(t)
	rig.conn.loseClosingAcks = 1 << 20 // every one of them
	rig.ran(t, "REQ-NOACK", "task-1")

	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	closing := 0
	for _, f := range frames[1:] {
		if f["finish"] != true || f["id"] != frames[0]["id"] || f["content"] != "the agent reply" {
			t.Fatalf("a frame after the opener is not the identical closing frame: %v", f)
		}
		closing++
	}
	if want := 1 + streamCloseRetries; closing != want {
		t.Fatalf("the closing frame was written %d time(s), want %d (the first and %d retries)", closing, want, streamCloseRetries)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("after the retries the answer was repeated as %d plain message(s): %v", len(pushes), pushes)
	}
	if got := mx.get("outbound_unconfirmed"); got != 1 {
		t.Errorf("outbound_unconfirmed = %d, want 1", got)
	}
	if got := mx.get("stream_finished"); got != 0 {
		t.Errorf("stream_finished = %d, want 0", got)
	}
}

// A verdict from the server is not a lost ack. 846608 on the first attempt
// means this stream will never take a frame: no retry, straight to the plain
// message.
//
// REVERSE VERIFICATION: make seal's loop continue on any error rather than on
// errStreamAckTimeout only, and this fails with four closing frames.
func TestARefusedClosingFrameIsNotRetried(t *testing.T) {
	t.Parallel()
	rig, mx := retryRig(t)
	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.ran(t, "REQ-REFUSED", "task-1")

	rig.answer(t, "the agent reply", "task-1")

	if frames := rig.conn.streamFrames(t); len(frames) != 2 {
		t.Fatalf("a refused closing frame was written %d time(s), want 1 — every retry is another refusal charged against the bot's rate limit", len(frames)-1)
	}
	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 || pushText(pushes[0]) != "the agent reply" {
		t.Fatalf("the answer did not arrive as one plain message: %v", pushes)
	}
	if got := mx.get("stream_fell_back"); got != 1 {
		t.Errorf("stream_fell_back = %d, want 1", got)
	}
}

// A delivery that took the bubble and then failed is one counted drop, and
// nothing follows from it: no bubble is left to seal, nothing is owed, and a
// replay of the completion goes out the way any bubble-less answer does — as a
// plain message, once.
//
// The socket is down when the words go out, so both the closing frame and the
// plain message find no sender (errNoLiveConnection: provably nothing
// written). That is main's semantics: the answer is dropped and counted, and
// the next event for this run finds a session with nothing on file.
//
// REVERSE VERIFICATION: make takeAtLocked leave the entry on the list (drop the
// removal of rounds[i]) and the replay finds the bubble again — a closing
// frame on the new socket where a plain message is expected, and depth 1
// where 0 is expected.
func TestATakeThatThenFailsIsOneCountedDropAndNothingMore(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	mx := newCountingMetrics()
	rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil, WithOutboundMetrics(mx))
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-DROP", "task-1")

	rig.senders.clear(rig.instID, rig.conn.sender)
	done := events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-1"),
		Payload:       protocol.ChatDonePayload{Content: "the agent reply"},
	}
	rig.out.handleEvent(done)

	if got := mx.get("outbound_dropped:no_live_connection"); got != 1 {
		t.Fatalf("dropped{no_live_connection} = %d, want 1 — an answer nobody could carry is one counted drop", got)
	}
	if got := mx.get("outbound_dropped"); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if got := rig.streams.depth(); got != 0 {
		t.Fatalf("store holds %d rounds after the failed delivery, want 0 — the bubble was taken by the attempt", got)
	}
	if rig.streams.has(bubbleSessionID(t), taskUUID(t, "task-1")) {
		t.Fatal("the run is still on file after its bubble was taken")
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 1 {
		t.Fatalf("the dead socket carried %d stream frames, want the opener only", len(frames))
	}

	// The socket comes back and the completion is replayed. No bubble, so it
	// is an ordinary message — once — and nothing on the old stream.
	next := rig.reconnect()
	rig.out.handleEvent(done)

	if got := next.streamFrames(t); len(got) != 0 {
		t.Fatalf("the replay wrote %d stream frame(s); the bubble was consumed by the first attempt and nothing should try it again", len(got))
	}
	pushes := next.pushes(t)
	if len(pushes) != 1 || pushText(pushes[0]) != "the agent reply" {
		t.Fatalf("the replay did not arrive as one plain message: %v", pushes)
	}
	if got := mx.get("outbound_delivered"); got != 1 {
		t.Errorf("delivered = %d, want 1", got)
	}
	if got := mx.get("outbound_dropped"); got != 1 {
		t.Errorf("dropped = %d after the replay, want still 1", got)
	}
}

// A closing frame the socket itself refuses to take — a broken pipe on the
// write — is not a lost ack, and it is not proof of non-delivery either.
// errWriteAttempted's own doc says the frame MAY have reached the peer: a
// half-closed connection reports "broken pipe" to the writer for bytes the
// reader already has. So the retry policy does not apply (it is for verdicts
// that never came) and neither does the fallback (it would print an answer the
// person may already be reading, in a chat with no unsend). One frame, no
// second copy, and the delivery recorded as what it is — unknown.
//
// This test used to assert the opposite while its own comment said this, which
// is the shape of a test that pins a defect.
//
// REVERSE VERIFICATION: make seal retry on any error and this fails with four
// closing frames; drop the errWriteAttempted arm from the fallback guard and
// it fails with a plain message the user reads as a duplicate.
func TestAClosingFrameTheSocketRefusesToTakeIsNotSaidTwice(t *testing.T) {
	t.Parallel()
	rig, mx := retryRig(t)
	rig.ran(t, "REQ-BROKEN", "task-1")
	rig.conn.failClosingWrite = errors.New("write: broken pipe")

	rig.answer(t, "the agent reply", "task-1")

	closing := 0
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			closing++
		}
	}
	if closing != 1 {
		t.Fatalf("%d closing frames reached the socket, want 1: a write the socket refused is not retried", closing)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the answer also went out as %d plain message(s): a write error is not proof the frame missed the peer, and WeCom has no unsend: %v", len(pushes), pushes)
	}
	if got := mx.get("outbound_unconfirmed"); got != 1 {
		t.Errorf("outbound_unconfirmed = %d, want 1 — written, no verdict, unknown", got)
	}
	if got := mx.get("stream_finished"); got != 0 {
		t.Errorf("stream_finished = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// what a lost ack is, and what it is not
// ---------------------------------------------------------------------------

// At the shipped constants the whole retry policy does not fit the budget it
// runs under — 3 retries need 26s against a 10s streamCloseTimeout — so the
// deadline, not the count, is what has to stop it. A retry started with less
// than one pause plus one ack wait left cannot finish: it burns the rest of
// the budget and returns ctx.Err() instead of the server's own answer, and the
// fallback that needed that budget has none.
//
// REVERSE VERIFICATION: drop the deadline check from seal's loop and this
// fails with the seal still writing after the budget is gone.
func TestTheCloseRetryStopsWhenTheBudgetCannotCoverAnother(t *testing.T) {
	t.Parallel()
	rig, _ := retryRig(t)
	rig.streams.closeRetryDelay = 40 * time.Millisecond
	rig.conn.loseClosingAcks = 1 << 20 // nothing is ever acked
	rig.ran(t, "REQ-FIT", "task-1")

	// Room for the first attempt and not much else: 50ms ack + 40ms pause +
	// 50ms ack is 140ms, so a second attempt does not fit in 120ms.
	budget, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	started := time.Now()
	rig.answerWithContext(t, budget, "the agent reply", "task-1")
	took := time.Since(started)

	if took > 120*time.Millisecond {
		t.Fatalf("the seal ran %s against a 120ms budget — it started an attempt it could not finish", took)
	}
	closing := 0
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			closing++
		}
	}
	if closing != 1 {
		t.Fatalf("the closing frame was written %d time(s); only the first fits this budget", closing)
	}
}

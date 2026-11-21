package wecom

// stream_ack_test.go — a turn writes several frames on ONE req_id, and the ack
// frame carries nothing but that req_id: no stream id, no sequence. So a
// verdict is only identifiable by its position in the turn's write order, and
// getting that wrong loses the user's answer silently.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// silentConn records writes and never answers them. Acks are fed in by the
// test, in the order the real read loop would deliver them.
type silentConn struct {
	mu     sync.Mutex
	frames []frameEnvelope
	err    error
	// deadlineErr fails SetWriteDeadline, which is the one failure raised
	// before any byte can leave. writeLocked wraps only the other kind in
	// errWriteAttempted, and the two must not be modelled by the same field:
	// one is provably unsent, the other may have reached the peer.
	deadlineErr error
}

func (c *silentConn) WriteMessage(_ int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.frames = append(c.frames, env)
	return nil
}
func (c *silentConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *silentConn) SetReadDeadline(time.Time) error   { return nil }
func (c *silentConn) SetWriteDeadline(time.Time) error  { return c.deadlineErr }
func (c *silentConn) Close() error                      { return nil }

func (c *silentConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

func (c *silentConn) streamBody(t *testing.T, i int) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.frames) {
		t.Fatalf("frame %d not written (have %d)", i, len(c.frames))
	}
	var body map[string]any
	if err := json.Unmarshal(c.frames[i].Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	stream, ok := body["stream"].(map[string]any)
	if !ok {
		t.Fatalf("frame %d is not a stream frame: %v", i, body)
	}
	return stream
}

// A sealed stream is immutable. A frame that lost the race to the answer must
// never reach the wire behind it — the server might still take it, and it would
// paint the placeholder back over the reply the user is reading.
func TestAFrameAfterTheClosingFrameNeverReachesTheWire(t *testing.T) {
	t.Parallel()
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))
	const reqID = "REQ-2"

	if err := sender.respondStream(context.Background(), reqID, "S-2", "the answer", true); err != nil {
		t.Fatalf("closing frame: %v", err)
	}
	before := len(conn.frames)

	err := sender.respondStream(context.Background(), reqID, "S-2", "a straggling refresh", false)
	if !errors.Is(err, errStreamSuperseded) {
		t.Fatalf("a frame written after the seal reported %v, want errStreamSuperseded", err)
	}
	if len(conn.frames) != before {
		t.Fatalf("the straggling frame reached the wire: %d frames, want %d", len(conn.frames), before)
	}
}

// A closing frame must NOT jump an opening frame whose ack is still in
// flight. Measured against the live bot (STRATEGY §6.4): two frames of one
// req_id on the wire are answered in whatever order the server likes, and the
// second is refused with 6000 for colliding with the first — so a jumped
// answer can be refused while its verdict is read off the opening frame's
// "accepted". The closing frame waits its turn instead.
func TestTheClosingFrameWaitsForTheUnackedOpeningFrame(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 2 * time.Second
	const reqID = "REQ-3"

	opening := make(chan error, 1)
	go func() {
		opening <- sender.respondStream(context.Background(), reqID, "S-3", streamThinkingPlaceholder, false)
	}()
	waitForFrames(t, conn, 1)

	closing := make(chan error, 1)
	go func() {
		closing <- sender.respondStream(context.Background(), reqID, "S-3", "the answer", true)
	}()

	// While the opening frame's verdict is outstanding the answer stays off
	// the wire.
	time.Sleep(150 * time.Millisecond)
	if n := conn.count(); n != 1 {
		t.Fatalf("%d frames on the wire with the opening frame unacked, want 1: the closing frame jumped", n)
	}
	select {
	case err := <-opening:
		t.Fatalf("opening frame returned %v before its verdict arrived", err)
	default:
	}

	// The opening frame's verdict arrives, and only now does the answer go.
	sender.deliverAck(reqID, 0, "")
	select {
	case err := <-opening:
		if err != nil {
			t.Fatalf("opening frame: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the opening frame never read its verdict")
	}
	waitForFrames(t, conn, 2)
	if got := conn.streamBody(t, 1)["finish"]; got != true {
		t.Errorf("second frame finish = %v, want true", got)
	}

	// The closing frame reads ITS verdict — a refusal here, the case that
	// used to be lost.
	sender.deliverAck(reqID, errcodeStreamExpired, "stream expired")
	select {
	case err := <-closing:
		if !streamUnusable(err) {
			t.Fatalf("closing frame reported %v; want the server's 846608 refusal so the answer falls back to a plain message", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the closing frame never read its verdict")
	}
}

// A non-final frame yields when the previous one on the same req_id has not
// been acked — the backpressure the official SDK calls replyStreamNonBlocking.
func TestANonFinalFrameYieldsToOneStillAwaitingItsVerdict(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 2 * time.Second
	const reqID = "REQ-4"

	first := make(chan error, 1)
	go func() {
		first <- sender.respondStream(context.Background(), reqID, "S-4", streamThinkingPlaceholder, false)
	}()
	waitForFrames(t, conn, 1)

	if err := sender.respondStream(context.Background(), reqID, "S-4", "another non-final frame", false); !errors.Is(err, errStreamBusy) {
		t.Fatalf("second non-final frame reported %v, want errStreamBusy", err)
	}
	if n := conn.count(); n != 1 {
		t.Fatalf("the yielding frame still reached the wire: %d frames, want 1", n)
	}
	sender.deliverAck(reqID, 0, "")
	<-first
}

// A write that never reached the socket must give its place back, or every
// later verdict on the turn is off by one — which is the misattribution above,
// arriving through a different door.
//
// The fixture is a SetWriteDeadline failure, because that is the whole of the
// population: writeLocked raises it before entering WriteMessage. It used to
// be a WriteMessage failure, which is the opposite case and is covered by the
// test below — the assertion was right and the fixture named the wrong door.
func TestAFailedWriteGivesBackItsPlaceInTheOrder(t *testing.T) {
	t.Parallel()
	conn := &silentConn{deadlineErr: errors.New("connection reset")}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 50 * time.Millisecond
	const reqID = "REQ-5"

	if err := sender.respondStream(context.Background(), reqID, "S-5", streamThinkingPlaceholder, false); err == nil {
		t.Fatal("a write that failed reported success")
	}
	sender.ackMu.Lock()
	st := sender.streams[reqID]
	sender.ackMu.Unlock()
	if st == nil {
		t.Fatal("no bookkeeping recorded for the turn")
	}
	if st.sent != 0 {
		t.Fatalf("sent = %d after a failed write, want 0; the next frame's verdict is now off by one", st.sent)
	}
}

// The other door, and the one that matters more: WriteMessage was entered, so
// the peer may have taken the bytes and may yet answer them. Giving the place
// back there would let that verdict settle the NEXT frame — the misattribution
// the counters exist to prevent, arriving through the fix for the case above.
func TestAWriteTheSocketMayHaveTakenKeepsItsPlaceInTheOrder(t *testing.T) {
	t.Parallel()
	conn := &silentConn{err: websocket.ErrCloseSent}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 50 * time.Millisecond
	const reqID = "REQ-5W"

	err := sender.respondStream(context.Background(), reqID, "S-5W", streamThinkingPlaceholder, false)
	if !errors.Is(err, errWriteAttempted) {
		t.Fatalf("respondStream returned %v, want it wrapped in errWriteAttempted", err)
	}
	sender.ackMu.Lock()
	st := sender.streams[reqID]
	sender.ackMu.Unlock()
	if st == nil {
		t.Fatal("no bookkeeping recorded for the turn")
	}
	if st.sent != 1 {
		t.Fatalf("sent = %d after a write the socket may have taken, want 1; a late verdict for it "+
			"would now be handed to the next frame", st.sent)
	}
}

// pruneStreamsLocked must never throw away a LIVE turn to stay under its cap.
// A live turn whose counters are gone has its next frame stamped from zero, and
// the whole misattribution above comes back — reinstated by the sweep meant to
// protect against it. A live turn is also the OLDEST entry by construction, so
// "evict oldest first" would pick exactly the wrong ones.
func TestPruneKeepsLiveTurnsEvenOverTheCap(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)

	// One live turn: a frame out, no verdict back, and young enough that the
	// round store still holds its handle.
	//
	// The fixture used to be 2*streamMaxAge old. That is not a live turn — past
	// streamMaxAge the store has evicted the handle and nothing can hand seal
	// one for this req_id — and keeping such an entry retains counters that
	// guard nothing. The property under test is unchanged: an entry that may
	// still take a frame survives the sweep however full the map is. The test
	// below covers the other side.
	const live = "REQ-LIVE"
	sender.streams[live] = &streamAcks{sent: 1, at: time.Now().Add(-streamMaxAge / 2)}

	// Fill past the cap with young sealed turns, which are the ones a burst
	// produces and the only ones that may be dropped.
	for i := 0; i < streamAcksMax; i++ {
		sender.streams[newReqID()] = &streamAcks{sealed: map[string]struct{}{"S": {}}, at: time.Now()}
	}
	sender.ackMu.Lock()
	sender.pruneStreamsLocked()
	sender.ackMu.Unlock()

	if _, ok := sender.streams[live]; !ok {
		t.Fatal("the sweep dropped a live turn; its next frame is stamped from zero and a stale verdict will settle its closing frame")
	}
}

// The other side: cancelAck leaves a debt on purpose, so a turn whose verdict
// never comes is never settled. Keeping it forever was the leak; retiring it on
// age alone was wrong for the reason recorded in pruneStreamsLocked. What
// retires it is OUR OWN reach ending — past streamMaxAge the round store has
// evicted the handle, and a closer already holding one is bounded by
// streamCloseTimeout, so no frame can be written for this req_id again.
func TestPruneRetiresATurnNothingCanWriteForAgain(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)

	const lost = "REQ-LOST"
	sender.streams[lost] = &streamAcks{
		sent: 1, acked: 0,
		at: time.Now().Add(-(streamMaxAge + streamCloseTimeout + time.Second)),
	}
	for i := 0; i < streamAcksMax; i++ {
		sender.streams[newReqID()] = &streamAcks{sealed: map[string]struct{}{"S": {}}, at: time.Now()}
	}
	sender.ackMu.Lock()
	sender.pruneStreamsLocked()
	sender.ackMu.Unlock()

	if _, ok := sender.streams[lost]; ok {
		t.Fatal("a turn past every window that could still write for it was kept; its counters " +
			"guard nothing and the map grows for the life of the connection")
	}
}

func waitForFrames(t *testing.T, c *silentConn, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.count() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("only %d frames written, want %d", c.count(), n)
}

// The inverted ordering is the one we MEASURED, and it is what makes position
// unusable once two frames are outstanding. STRATEGY §6.4: twenty-four frames
// written back-to-back came back grouped by outcome rather than in write order.
// So the closing frame's own refusal can land before an abandoned opener's late
// acceptance, and matching by position drops the refusal and hands the
// acceptance to the closing frame — the answer recorded as delivered, no
// fallback, nobody receives it.
//
// The gate removes the premise rather than trying to survive it: while the
// server still owes a verdict on this req_id, no further frame is written.
// When the abandoned verdict never comes, the closing frame is never written
// either, and the caller is told so plainly enough to send the answer the
// ordinary way — which is the route that always works.
//
// REVERSE VERIFICATION: drop the owed check from awaitAck and this fails with
// two frames on the wire and the closing one reporting nil, which is the
// answer recorded as delivered and sent nowhere.
func TestNoFrameIsWrittenWhileTheServerStillOwesAVerdict(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 60 * time.Millisecond
	const reqID = "REQ-OWED"

	// The opening frame is abandoned by its caller — on the reply path that is
	// the router's replyTimeout, 2500ms, well under ackTimeout, so it happens
	// on an ordinary turn rather than a slow one. Its verdict never arrives.
	if err := sender.respondStream(context.Background(), reqID, "S-1", streamThinkingPlaceholder, false); !errors.Is(err, errStreamAckTimeout) {
		t.Fatalf("opening frame: got %v, want errStreamAckTimeout", err)
	}

	err := sender.respondStream(context.Background(), reqID, "S-1", "the agent reply", true)
	if err == nil {
		t.Fatal("the closing frame reported success while the server still owed a verdict: whatever ack arrives next would have been read as its own")
	}
	if !provablyNotSent(err) {
		t.Fatalf("closing frame reported %v, which is not provably unsent — the answer would be left as unknown when in fact no frame was written", err)
	}
	if n := len(conn.frames); n != 1 {
		t.Fatalf("%d frames reached the socket, want 1: the second was written with two verdicts outstanding", n)
	}
}

// The other half: once the abandoned frame's verdict does arrive, the server
// owes nothing, position means what it says again, and the closing frame goes
// out and reads its own verdict.
func TestTheClosingFrameGoesOutOnceTheOwedVerdictArrives(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 2 * time.Second
	const reqID = "REQ-SETTLED"

	if err := sender.respondStream(context.Background(), reqID, "S-1", streamThinkingPlaceholder, false); err == nil {
		t.Fatal("the opening frame should have timed out")
	}
	// The opener's verdict lands after its caller gave up. Nobody reads it;
	// it only settles the debt.
	sender.deliverAck(reqID, 0, "")

	done := make(chan error, 1)
	go func() {
		done <- sender.respondStream(context.Background(), reqID, "S-1", "the agent reply", true)
	}()
	waitForFrames(t, conn, 2)
	sender.deliverAck(reqID, errcodeStreamExpired, "stream expired")

	select {
	case err := <-done:
		if !streamUnusable(err) {
			t.Fatalf("closing frame reported %v; want the server's 846608 refusal, which is its own", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the closing frame never read its verdict")
	}
}

// An entry that was never sealed used to live for the life of the connection:
// prune only retired entries that were BOTH sealed and old, so a turn whose
// closing frame never went out — the gate refused it while a verdict was owed,
// the answer fell back — left its counters behind for good.
//
// Settled is the condition that matters, not sealed: with acked == sent the
// server owes this req_id nothing, so no verdict can arrive later to be read
// against counters that were reset. An entry still owed one stays however old
// it is — that is the misattribution the sequence numbers exist to prevent,
// and age cannot rule it out, because a new stream id on the same req_id is
// accepted after the stream itself has expired (STRATEGY §6.1).
//
// REVERSE VERIFICATION: put the len(st.sealed) > 0 condition back and this
// fails with the unsealed entries still there.
func TestBookkeepingForATurnThatNeverSealedIsStillRetired(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)

	sender.ackMu.Lock()
	for i := 0; i < streamAcksMax+1; i++ {
		// The opening frame was answered and no closing frame ever went out:
		// the shape a fallback leaves behind, settled but never sealed.
		sender.streams[fmt.Sprintf("REQ-OLD-%d", i)] = &streamAcks{
			sent: 1, acked: 1, at: time.Now().Add(-streamMaxAge - time.Minute),
		}
	}
	sender.ackMu.Unlock()

	sender.ackMu.Lock()
	sender.pruneStreamsLocked()
	left := len(sender.streams)
	sender.ackMu.Unlock()

	if left != 0 {
		t.Fatalf("%d entries survived, want 0 — a turn whose closing frame never went out keeps its counters for the life of the connection", left)
	}
}

// ---- the gate lets a rewrite out; it must also let the rewrite's verdict in ----

// A closing frame whose ack was lost leaves a permanent debt, on purpose:
// cancelAck never writes off a verdict, because a late one must not settle the
// NEXT frame. The owed gate reads that debt and blocks new frames. seal's
// retry is the one exemption — an identical frame is not a second frame.
//
// The exemption has to cover BOTH directions. Letting the frame onto the wire
// while stamping its waiter unaddressable is strictly worse than refusing it:
// the server's answer cannot be matched, seal reads a timeout in place of the
// refusal that actually came back, and a stated refusal is the only signal
// that routes the answer to a plain message. The user then gets nothing.
func TestARewriteReadsItsOwnVerdictDespiteTheDebtThatLetItOut(t *testing.T) {
	t.Parallel()
	conn := &silentConn{}
	sender := newWSSender(conn, nil)
	sender.ackTimeout = 200 * time.Millisecond
	const reqID, streamID = "REQ-RW", "S-RW"

	// A frame goes out and its verdict never comes: sent=1, acked=0 forever.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := sender.respondStream(ctx, reqID, streamID, "first", true); !errors.Is(err, errStreamAckTimeout) &&
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the abandoned frame returned %v, want it to give up waiting", err)
	}
	sender.ackMu.Lock()
	st := sender.streams[reqID]
	debt := st.sent - st.acked
	sender.ackMu.Unlock()
	if debt == 0 {
		t.Fatal("no debt left behind; this test's whole premise is the debt the owed gate reads")
	}

	// The retry, on the SAME socket — which is what production does, because
	// sendersRegistry.streamRewrite resolves by installation id and nothing
	// reconnects. The server refuses it, with words.
	done := make(chan error, 1)
	go func() {
		done <- sender.respondStreamRewrite(context.Background(), reqID, streamID, "first", true)
	}()
	// Wait for the frame to be STAMPED — seq is set inside the writer, after
	// the waiter is registered — and only then read the verdict it may match.
	deadline := time.After(2 * time.Second)
	for {
		sender.ackMu.Lock()
		w, waiting := sender.waiters[reqID]
		stamped := waiting && w.seq != 0
		addressable := stamped && w.addressable
		sender.ackMu.Unlock()
		if stamped {
			if !addressable {
				t.Fatal("the rewrite was written unaddressable: the gate let the frame out and " +
					"then blinded it to the answer, so a stated refusal is read as a timeout and " +
					"the answer is recorded unconfirmed while nothing reached the bubble")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("the rewrite never reached the wire")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// Settle the debt, then answer the rewrite — the order a server sending
	// both verdicts produces.
	sender.deliverAck(reqID, 0, "")
	sender.deliverAck(reqID, 45002, "content too long")

	select {
	case err := <-done:
		var refusal *streamError
		if !errors.As(err, &refusal) || refusal.Code != 45002 {
			t.Fatalf("the rewrite returned %v, want the server's stated refusal (45002) — a refusal "+
				"read as a timeout is classified unconfirmed and the answer is never said", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the rewrite never read its verdict")
	}
}

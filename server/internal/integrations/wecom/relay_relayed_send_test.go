package wecom

// relay_relayed_send_test.go — what the LEASE HOLDER does with a frame another
// replica routed to it, driven through the REAL Outbound.deliverRelayed and the
// real dispatcher (DeliverWecomOutbound → perform → step).
//
// That is the whole point of this file. relay_ordering_db_test.go covers the
// dispatcher against a fake handler that returns an outcome directly
// (failsOnceHandler), so every property that lives INSIDE deliverRelayed — which
// counter moves, whether the claim goes back, what the socket actually received
// — was invisible to it: four rounds of tests passed while a single routed reply
// was recording a drop on every retry, and while a long answer's first piece was
// being printed twice.
//
// So the handler here is a real *Outbound over a real wsSender, and the socket
// double decides what fails. No database and no Redis: dedupe is nil, which is
// the single-replica claim gate, and the retry chain is the same code either way.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// ---------------------------------------------------------------------------
// the socket double
// ---------------------------------------------------------------------------

// errSocketClosed is the failure this file is built around: SetWriteDeadline
// refusing on a socket that has already gone. It is raised BEFORE
// WriteMessage is entered, so ws_sender does not wrap it in errWriteAttempted
// and provablyNotSent reads it as "nothing left this process" — which is what
// makes the dispatcher offer the frame again, and what made every one of those
// offers record its own drop.
var errSocketClosed = errors.New("use of closed network connection")

// deadlineFlakyConn is a socket that acks everything it is allowed to write,
// and refuses the write deadline on the calls a test names. Refusing there
// rather than in WriteMessage is deliberate: it is the only way to produce a
// provably-unsent failure, and a test that used WriteMessage would be
// exercising the retry-safe path instead.
type deadlineFlakyConn struct {
	mu       sync.Mutex
	sender   *wsSender
	texts    []string
	attempts int
	// failOn reports whether the n-th (1-based) write deadline is refused.
	failOn func(n int) bool

	// refuseFromSend and swallowAckFromSend act on aibot_send_msg frames,
	// counted 1-based, so a test can refuse or lose the verdict on the SECOND
	// piece of a split answer after the first one landed. Unlike failOn these
	// are failures the peer stated or swallowed, not ones raised before the
	// write — which is what makes the send partial rather than unsent.
	refuseFromSend     int
	swallowAckFromSend int
	sends              int
}

func (c *deadlineFlakyConn) newSender() *wsSender {
	s := newWSSender(c, testLogger())
	c.mu.Lock()
	c.sender = s
	c.mu.Unlock()
	return s
}

func (c *deadlineFlakyConn) SetWriteDeadline(time.Time) error {
	c.mu.Lock()
	c.attempts++
	n, fail := c.attempts, c.failOn
	c.mu.Unlock()
	if fail != nil && fail(n) {
		return errSocketClosed
	}
	return nil
}

func (c *deadlineFlakyConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	var body struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	_ = json.Unmarshal(env.Body, &body)
	c.mu.Lock()
	code, msg, swallow := 0, "", false
	if env.Cmd == cmdSendMsg {
		c.sends++
		if c.refuseFromSend > 0 && c.sends >= c.refuseFromSend {
			code, msg = 45002, "content exceed max length"
		}
		swallow = c.swallowAckFromSend > 0 && c.sends >= c.swallowAckFromSend
		if code == 0 && body.MsgType == "markdown" {
			c.texts = append(c.texts, body.Markdown.Content)
		}
	}
	s := c.sender
	c.mu.Unlock()
	if s != nil && !swallow {
		s.routeResponse(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}, ErrCode: code, ErrMsg: msg})
	}
	return nil
}

func (c *deadlineFlakyConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *deadlineFlakyConn) SetReadDeadline(time.Time) error   { return nil }
func (c *deadlineFlakyConn) Close() error                      { return nil }

// sent is every markdown push the chat actually received, in order.
func (c *deadlineFlakyConn) sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// writeAttempts counts how many times a frame was offered to the socket, which
// is how many times deliverRelayed ran.
func (c *deadlineFlakyConn) writeAttempts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

// ---------------------------------------------------------------------------
// the rig
// ---------------------------------------------------------------------------

// relaySendRig is one lease holder: a real subscriber over a socket that can be
// made to fail, wired behind the real dispatcher.
type relaySendRig struct {
	o      *Outbound
	router *RelayOutbound
	conn   *deadlineFlakyConn
	mx     *countingMetrics
	instID pgtype.UUID
	cancel context.CancelFunc
}

// relayRetryConfig is a whole retry chain measured in milliseconds, so a test
// can watch one run out. LeaseSettle/RetryBackoff are the only two knobs the
// chain is built from, and retryPlan is asked for the length rather than told.
var relayRetryConfig = RelayConfig{Shards: 1, LeaseSettle: 40 * time.Millisecond, RetryBackoff: 5 * time.Millisecond, DeliveryBudget: 20 * time.Millisecond}

func newRelaySendRig(t *testing.T, failOn func(n int) bool) *relaySendRig {
	t.Helper()
	return newRelaySendRigWithDedupe(t, failOn, nil)
}

// newRelaySendRigWithDedupe is the rig with a claim store, for the paths the
// claim gate takes part in.
func newRelaySendRigWithDedupe(t *testing.T, failOn func(n int) bool, dedupe DedupeStore) *relaySendRig {
	t.Helper()
	return newRelaySendRigWithConfig(t, failOn, dedupe, relayRetryConfig)
}

// newRelaySendRigWithConfig is the rig with the chain sized by the test, for
// the ones that have to watch a whole re-offer chain run against something
// slower than a millisecond.
func newRelaySendRigWithConfig(t *testing.T, failOn func(n int) bool, dedupe DedupeStore, cfg RelayConfig) *relaySendRig {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &deadlineFlakyConn{failOn: failOn}
	reg.set(instID, conn.newSender())

	mx := newCountingMetrics()
	o := NewOutbound(&fakeOutboundQueries{}, reg, testLogger(), WithOutboundMetrics(mx))
	o.spawn = func(f func()) { f() }

	// No dedupe store: that is the single-replica claim gate, and it leaves the
	// retry chain — the thing under test — exactly as it is in production.
	router := NewRelayOutbound(&fanoutRelay{}, dedupe, cfg, testLogger())
	router.SetMetrics(mx)
	router.Attach(o)
	ctx, cancel := context.WithCancel(context.Background())
	router.Start(ctx)
	t.Cleanup(func() {
		cancel()
		router.Wait()
	})
	return &relaySendRig{o: o, router: router, conn: conn, mx: mx, instID: instID, cancel: cancel}
}

// route hands the rig a reply the way another replica's publish would.
func (r *relaySendRig) route(t *testing.T, content string) {
	t.Helper()
	body, err := json.Marshal(relayFrame{
		Kind:           relayKindReply,
		InstallationID: util.UUIDToString(r.instID),
		ChatID:         "CHAT_1",
		ChatType:       chatTypeGroupInt,
		Content:        content,
		SessionID:      testSessionID,
		TaskID:         testTaskID,
	})
	if err != nil {
		t.Fatalf("marshal relay frame: %v", err)
	}
	r.router.DeliverWecomOutbound(util.UUIDToString(r.instID), body, "ev-1")
}

// lastEventID is the id route hands every frame, which is what its claim is
// keyed on.
func (r *relaySendRig) lastEventID() string { return "ev-1" }

// stop cancels the dispatcher and waits for it, which drains whatever is parked
// waiting out a backoff. Used where the assertion is that NOTHING is parked:
// the drain performs a parked frame immediately, so a test that stops here
// either sees the re-offer or proves there was none.
func (r *relaySendRig) stop() {
	r.cancel()
	r.router.Wait()
}

// ---------------------------------------------------------------------------
// 1. a frame still in flight has no outcome yet
// ---------------------------------------------------------------------------

// A routed reply that cannot reach the wire is RE-OFFERED, and a frame that
// will be offered again is not an outcome. Recording one per attempt puts the
// same reply on outbound_dropped once for every link in the retry chain —
// twelve times on the production defaults, plus a thirteenth from the
// publisher's own settle — and turns the drop counter into a count of retries.
//
// The single owner of a routed reply's fate is the publisher's watchOutcomes,
// which asks after the fact whether ANY replica claimed it and counts the loss
// once. Nothing in here may pre-empt that.
//
// REVERSE VERIFICATION: move the reply counters back above the provablyNotSent
// check in deliverRelayed and this test reports outbound_dropped = 8 (one per
// attempt). `go build`, `go vet` and `go test -race` on the rest of the package
// all stay silent under that revert — the defect is a counter reading, not a
// type error, and no existing test drives deliverRelayed's retry path at all.
func TestRelayedReply_AFrameStillInFlightRecordsNoReplyOutcome(t *testing.T) {
	t.Parallel()
	rig := newRelaySendRig(t, func(int) bool { return true }) // the socket never takes anything
	wantAttempts := len(relayRetryConfig.retryPlan()) + 1     // the first offer, then the chain

	rig.route(t, "答案")
	waitFor(t, "the retry chain to run out", func() bool {
		return rig.conn.writeAttempts() >= wantAttempts
	})
	rig.stop()

	if got := rig.conn.writeAttempts(); got != wantAttempts {
		t.Fatalf("delivery attempts = %d, want %d — the dispatcher's own chain", got, wantAttempts)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — this reply was re-offered %d times and "+
			"counting each one turns the drop counter into a retry counter; the publisher's "+
			"watchOutcomes is the one owner that settles a routed reply, once", got, wantAttempts)
	}
	if got := rig.mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0 — same rule, other counter", got)
	}
	if got := rig.mx.get("outbound_delivered"); got != 0 {
		t.Errorf("outbound_delivered = %d, want 0 — nothing reached the chat", got)
	}
}

// The other half of the same rule: a reply the chain eventually delivers is
// counted delivered, and must not ALSO appear as a drop. One reply on both
// counters at once is the defect shed's comment claims this package no longer
// has, and the retry path was reproducing it.
//
// REVERSE VERIFICATION: with the counters back above the provablyNotSent check
// this reports outbound_dropped = 1 alongside outbound_delivered = 1.
func TestRelayedReply_ARetryThatSucceedsIsNotAlsoADrop(t *testing.T) {
	t.Parallel()
	rig := newRelaySendRig(t, func(n int) bool { return n == 1 }) // the first offer only

	rig.route(t, "答案")
	waitFor(t, "the reply to reach the chat", func() bool { return len(rig.conn.sent()) == 1 })
	rig.stop()

	if got := rig.conn.sent(); len(got) != 1 || got[0] != "答案" {
		t.Fatalf("the chat received %q, want the one answer", got)
	}
	if got := rig.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1", got)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — the same reply cannot be delivered and "+
			"dropped at once", got)
	}
}

// ---------------------------------------------------------------------------
// 2. a re-offer must not repeat what the user already read
// ---------------------------------------------------------------------------

// A release whose result is UNKNOWN — the DEL never reached the server, the
// key still holds this replica's token — is not a verdict. The next offer's
// Claim finds its own token and re-takes the claim, the delivery runs again,
// and the reply ends with one record: delivered.
//
// REVERSE VERIFICATION: make Claim refuse a key that holds the caller's own
// token (drop the `v == ARGV[1]` / `v == token` branch) and this fails: every
// re-offer loses the claim and nothing is ever delivered or counted.
func TestRelayedReply_AReleaseWhoseResultIsUnknownIsReclaimedByTheNextOffer(t *testing.T) {
	t.Parallel()
	dedupe := newSharedDedupe()
	dedupe.releaseFails = true // the DEL never lands; the key keeps our token
	rig := newRelaySendRigWithDedupe(t, func(n int) bool { return n == 1 }, dedupe)

	rig.route(t, "the agent reply")
	waitFor(t, "the re-offer to deliver", func() bool {
		return rig.mx.get("outbound_delivered") == 1
	})
	time.Sleep(rig.router.outcomeGrace())

	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("outbound_dropped = %d, want 0: an unknown release is not a loss", got)
	}
	if got := rig.conn.writeAttempts(); got != 2 {
		t.Fatalf("%d offers, want 2: the failed one and the re-claimed one", got)
	}
	if dedupe.heldCount() != 1 || dedupe.valueOf(dedupeKey(rig.lastEventID())) != claimSettledValue {
		t.Fatalf("claim store holds %d key(s) with value %q, want the one key settled by its holder",
			dedupe.heldCount(), dedupe.valueOf(dedupeKey(rig.lastEventID())))
	}
}

// The other face of an unknown release: the DEL DID land and only its response
// was lost. The key is gone, the next offer's Claim takes it fresh, and the
// reply again ends with exactly one record. Nothing was recorded on the
// strength of the error — that is the whole point.
//
// REVERSE VERIFICATION: record a drop on a Release error in perform (the
// round-2 shape) and this fails with outbound_dropped = 1 beside
// outbound_delivered = 1.
func TestRelayedReply_AReleaseThatLandedButErroredIsTakenFreshByTheNextOffer(t *testing.T) {
	t.Parallel()
	dedupe := newSharedDedupe()
	dedupe.releaseErrAfterDelete = true
	rig := newRelaySendRigWithDedupe(t, func(n int) bool { return n == 1 }, dedupe)

	rig.route(t, "the agent reply")
	waitFor(t, "the re-offer to deliver", func() bool {
		return rig.mx.get("outbound_delivered") == 1
	})
	time.Sleep(rig.router.outcomeGrace())

	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("outbound_dropped = %d, want 0", got)
	}
	if got := rig.conn.writeAttempts(); got != 2 {
		t.Fatalf("%d offers, want 2", got)
	}
}

// The boundary the settle retry deliberately stops at, pinned so it can only
// move on purpose.
//
// Every attempt here executes the settle and loses its answer, so the store
// ends up settled while the holder never learns it did. The holder cannot tell
// that from a settle that never ran, and the two want opposite records — so it
// makes none. What the user got is unaffected: the reply reached the chat
// once, and an unconfirmed settle never re-sends it.
//
// This is the accepted cost of not double-counting the far more common case
// where the settles never landed and the publisher ends the reply itself
// (TestTwoReplicas_ASettleNobodyCanCompleteIsEndedOnceByThePublisher covers
// that side, publisher included). A store failing this way this long is a
// monitoring gap, not a lost answer, and settleClaim logs a warning naming it.
func TestRelayedReply_ASettleThatNeverConfirmsLeavesTheOutcomeUnrecorded(t *testing.T) {
	t.Parallel()
	dedupe := newSharedDedupe()
	dedupe.settleErrAfterWrite = claimSettleAttempts
	rig := newRelaySendRigWithDedupe(t, nil, dedupe)

	rig.route(t, "the agent reply")
	waitFor(t, "the settled state the holder never gets to hear about", func() bool {
		return dedupe.valueOf(dedupeKey(rig.lastEventID())) == claimSettledValue
	})
	time.Sleep(rig.router.outcomeGrace())

	if got := rig.conn.writeAttempts(); got != 1 {
		t.Fatalf("%d offers, want 1: the reply reached the chat, and an unconfirmed settle must not re-send it", got)
	}
	if got := rig.mx.get("outbound_delivered") + rig.mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("the holder recorded %d outcome(s), want 0: it cannot know whether its settle landed", got)
	}
}

// A settle whose request never reached the store is retried, and the retry
// settles it. The holder records once — no reliance on the publisher, which
// would have counted this delivered reply as a drop.
//
// REVERSE VERIFICATION: drop the retry loop from settleClaim and this fails
// with outbound_delivered = 0.
func TestRelayedReply_ASettleThatNeverExecutedIsRetried(t *testing.T) {
	t.Parallel()
	dedupe := newSharedDedupe()
	dedupe.settleErrBeforeWrite = 1
	rig := newRelaySendRigWithDedupe(t, nil, dedupe)

	rig.route(t, "the agent reply")
	waitFor(t, "the delivery to be recorded after the settle retry", func() bool {
		return rig.mx.get("outbound_delivered") == 1
	})
	time.Sleep(rig.router.outcomeGrace())

	if got := rig.mx.get("outbound_delivered"); got != 1 {
		t.Fatalf("outbound_delivered = %d, want 1", got)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("outbound_dropped = %d, want 0", got)
	}
	if got := rig.conn.writeAttempts(); got != 1 {
		t.Fatalf("%d offers, want 1: a settle retry is not a re-delivery", got)
	}
	if v := dedupe.valueOf(dedupeKey(rig.lastEventID())); v != claimSettledValue {
		t.Fatalf("claim value = %q, want %q", v, claimSettledValue)
	}
}

// ---------------------------------------------------------------------------
// the same partial send, on the lease holder
// ---------------------------------------------------------------------------

// The relay half of the pair in long_reply_test.go. A reply produced on
// another replica, split because it is over the cap, with its second piece
// refused here: the person's screen is in exactly the state the direct path
// leaves it in, so the counter has to agree.
//
// It also must not be re-offered. provablyNotSent reads errPartiallySent as
// "something reached the peer", and a retry would print piece one a second
// time in a chat with no unsend.
//
// REVERSE VERIFICATION: drop the errPartiallySent arm from recordSend and this
// reports outbound_dropped = 1 with outbound_delivered = 0 — the very split
// this test exists to close, since the direct path counts the same event
// delivered.
func TestRelayedReply_ARefusedSecondPieceCountsDeliveredAndIsNotReoffered(t *testing.T) {
	t.Parallel()
	rig := newRelaySendRig(t, nil)
	rig.conn.refuseFromSend = 2

	rig.route(t, aLongAnswer())
	waitFor(t, "the lease holder to take the reply", func() bool {
		return rig.mx.get("outbound_delivered")+rig.mx.get("outbound_dropped")+rig.mx.get("outbound_unconfirmed") > 0
	})
	rig.stop()

	if got := rig.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1 — the direct path counts this same event delivered", got)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — a drop here and a delivery on the direct path is the same reply with two verdicts", got)
	}
	if got := rig.mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0", got)
	}
	if got := len(rig.conn.sent()); got != 1 {
		t.Errorf("the chat received %d piece(s), want 1 — piece two was refused and must not be re-offered", got)
	}
}

// The lost-verdict half. The dispatcher gives a delivery a bounded budget, so
// the wait ends there rather than on ackTimeout, and the frame is still not
// owed another offer: piece one is already on the person's screen.
//
// REVERSE VERIFICATION: same revert, and this reports outbound_unconfirmed = 1
// with outbound_delivered = 0.
func TestRelayedReply_ALostAckOnTheSecondPieceCountsDelivered(t *testing.T) {
	t.Parallel()
	rig := newRelaySendRig(t, nil)
	rig.conn.swallowAckFromSend = 2

	rig.route(t, aLongAnswer())
	// Both pieces reach the wire; the second one's verdict never comes back.
	// What ends that wait is the delivery's own deadline — DeliveryBudget,
	// 20ms on this rig — and here the dispatcher shutting down gets there
	// first. Either way it is the same ctx.Err() out of request(), without a
	// test that stands still for the five-second ackTimeout.
	waitFor(t, "both pieces to reach the wire", func() bool {
		return rig.conn.writeAttempts() >= 2
	})
	rig.stop()

	if got := rig.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1", got)
	}
	if got := rig.mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0 — part of the answer is on screen, so the delivery is not unknown", got)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0", got)
	}
}

// A routed reply that never reached the socket because the chat was busy is
// OFFERED AGAIN, and arrives. This is the retryable failure of the whole set:
// sendTextCtx takes the chat's turn before it builds a frame, so a delivery
// whose budget runs out while queued put nothing anywhere.
//
// It used to end the reply. acquire returned a bare ctx.Err(), provablyNotSent
// read that as "may have been sent", and the dispatcher settled the claim and
// stopped — a message the user never received, with the one party that could
// have re-sent it told not to.
//
// The lock is held here for less than the retry chain lasts, which is the real
// shape of the thing: the chat is busy with the answer before this one, not
// broken.
//
// REVERSE VERIFICATION: return ctx.Err() bare from chatLocks.acquire again and
// this fails with "timed out waiting for the reply to reach the chat", the
// chat having received nothing and outbound_unconfirmed = 1.
func TestRelayedReply_AReplyThatNeverGotTheChatsTurnIsOfferedAgain(t *testing.T) {
	t.Parallel()
	// Its own chain rather than the millisecond one the rest of the file uses,
	// sized by the two things this test needs to keep apart. The chat is busy
	// for LONGER than one delivery budget, so the first offers really do give
	// up waiting — that is the failure under test — and for far less than the
	// whole re-offer chain, so a later offer still has one to spare. Ten
	// offers over ~1.3s of backoff against a chat busy for 120ms.
	budget := 40 * time.Millisecond
	rig := newRelaySendRigWithConfig(t, nil, nil, RelayConfig{
		Shards: 1, LeaseSettle: 800 * time.Millisecond,
		RetryBackoff: 20 * time.Millisecond, DeliveryBudget: budget,
	})

	// Somebody else has the chat's turn — an answer already going out on this
	// socket. The first offer's budget runs out inside the wait.
	release, err := rig.conn.sender.chats.acquire(context.Background(), "CHAT_1")
	if err != nil {
		t.Fatalf("taking the chat's turn: %v", err)
	}
	go func() {
		time.Sleep(3 * budget)
		release()
	}()

	rig.route(t, "答案")
	waitFor(t, "the reply to reach the chat on a later offer", func() bool {
		return len(rig.conn.sent()) > 0
	})
	rig.stop()

	if got := rig.conn.sent(); len(got) != 1 {
		t.Fatalf("the chat received %d message(s), want 1: %v", len(got), got)
	}
	if got := rig.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1", got)
	}
	if got := rig.mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0 — a delivery that never got the chat's turn wrote nothing, so its outcome is not unknown", got)
	}
	if got := rig.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — the frame was re-offered and arrived", got)
	}
}

// DeliveryBudget is the number outcomeGrace is computed from, so a delivery
// that outlives it makes the publisher's grace a wrong answer: a Resolve
// landing inside an ack wait fences a reply that is still being written. It
// was documented as the bound on "the send and its ack wait" and never
// applied — the only limit the send had was ackTimeout, the constant.
//
// The socket here writes and never answers, which before this was a five
// second wait whatever the config said.
//
// REVERSE VERIFICATION: hand deliverRelayed the dispatcher's ctx again and
// this fails with the delivery taking ackTimeout (5s) against a 120ms budget.
func TestADeliveryIsBoundedByTheBudgetItsGraceIsComputedFrom(t *testing.T) {
	t.Parallel()
	budget := 120 * time.Millisecond
	cfg := RelayConfig{Shards: 1, LeaseSettle: 40 * time.Millisecond, RetryBackoff: 5 * time.Millisecond, DeliveryBudget: budget}

	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &silentAckConn{}
	reg.set(instID, conn.newSender())
	mx := newCountingMetrics()
	o := NewOutbound(&fakeOutboundQueries{}, reg, testLogger(), WithOutboundMetrics(mx))
	o.spawn = func(f func()) { f() }
	router := NewRelayOutbound(&fanoutRelay{}, nil, cfg, testLogger())
	router.SetMetrics(mx)
	router.Attach(o)
	ctx, cancel := context.WithCancel(context.Background())
	router.Start(ctx)
	t.Cleanup(func() { cancel(); router.Wait() })

	body, err := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeGroupInt, Content: "答案",
		SessionID: testSessionID, TaskID: testTaskID,
	})
	if err != nil {
		t.Fatalf("marshal relay frame: %v", err)
	}
	started := time.Now()
	router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-budget")
	// The delivery is over when its outcome is filed. A send cut by the
	// budget ends in a context error, which unconfirmedReason reads as
	// unknown — the frame went out and no verdict came back.
	waitFor(t, "the delivery to record an outcome inside its budget rather than at ackTimeout", func() bool {
		return mx.get("outbound_unconfirmed")+mx.get("outbound_dropped")+mx.get("outbound_delivered") > 0
	})
	took := time.Since(started)

	if conn.attempts() == 0 {
		t.Fatal("nothing was written; this test is not measuring a delivery")
	}
	if got := mx.get("outbound_unconfirmed"); got != 1 {
		t.Errorf("outbound_unconfirmed = %d, want 1 — a delivery cut by its own budget is unknown, not failed", got)
	}
	// ackTimeout is five seconds. Anything near it means the budget was not
	// applied; a small multiple of the budget is scheduling noise.
	if took > 2*time.Second {
		t.Fatalf("the delivery took %s against a %s budget — the send is still bounded by ackTimeout, not by the number the grace is computed from", took, budget)
	}
}

// ONE budget covers the whole logical delivery — the wait for the chat's turn
// and every piece of the answer — which is what makes the publisher's grace an
// answer again.
//
// Splitting moved the arithmetic under outcomeGrace. Before it, a text
// delivery waited on exactly one ack, so an offer's delivery cost one ack
// wait. After it, one logical send waits for the chat's turn and then for N
// consecutive acks, while the grace reserves one budget per offer — so a slow
// multi-piece answer outlives its offer's budget and Resolve fences a reply
// whose holder is still writing it.
//
// The cap is applied once, around deliverRelayed, so everything inside — the
// lock wait and all the pieces — shares the one budget the grace sets aside.
// Here the chat is busy for the first 60ms and the third piece is never
// acknowledged, and the whole thing still ends inside one budget.
//
// REVERSE VERIFICATION: hand deliverRelayed the dispatcher's ctx again and
// this fails with "timed out waiting for the delivery to end inside one
// budget" — the unacknowledged piece waits out ackTimeout, five seconds,
// against a 300ms budget.
func TestASlowMultiPieceDeliveryFitsInTheOneBudgetTheGraceReserves(t *testing.T) {
	t.Parallel()
	budget := 300 * time.Millisecond
	cfg := RelayConfig{Shards: 1, LeaseSettle: 40 * time.Millisecond, RetryBackoff: 5 * time.Millisecond, DeliveryBudget: budget}

	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &slowAckConn{delay: 40 * time.Millisecond, swallowFrom: 3}
	sender := newWSSender(conn, testLogger())
	conn.sender = sender
	reg.set(instID, sender)
	mx := newCountingMetrics()
	o := NewOutbound(&fakeOutboundQueries{}, reg, testLogger(), WithOutboundMetrics(mx))
	o.spawn = func(f func()) { f() }
	router := NewRelayOutbound(&fanoutRelay{}, nil, cfg, testLogger())
	router.SetMetrics(mx)
	router.Attach(o)
	ctx, cancel := context.WithCancel(context.Background())
	router.Start(ctx)
	t.Cleanup(func() { cancel(); router.Wait() })

	// The chat is busy when the delivery starts: the budget has to cover the
	// wait for its turn, not only the sending.
	release, err := sender.chats.acquire(context.Background(), "CHAT_1")
	if err != nil {
		t.Fatalf("taking the chat's turn: %v", err)
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		release()
	}()

	body, err := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeGroupInt, Content: aLongAnswer(),
		SessionID: testSessionID, TaskID: testTaskID,
	})
	if err != nil {
		t.Fatalf("marshal relay frame: %v", err)
	}
	started := time.Now()
	router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-slow-split")
	waitFor(t, "the delivery to end inside one budget rather than on the ack timeout of one piece", func() bool {
		return mx.get("outbound_delivered")+mx.get("outbound_unconfirmed")+mx.get("outbound_dropped") > 0
	})
	took := time.Since(started)

	if got := len(conn.wire()); got < 3 {
		t.Fatalf("%d piece(s) reached the wire, want at least 3 — this is not measuring a multi-piece delivery", got)
	}
	// Generous against scheduling noise and still nowhere near the five
	// seconds an unbounded delivery spends on the piece nobody acknowledges.
	if limit := budget + 400*time.Millisecond; took > limit {
		t.Fatalf("the delivery took %s against a %s budget — the pieces after the first are outside the bound the grace is computed from", took, budget)
	}
	if grace := router.outcomeGrace(); grace <= took {
		t.Errorf("outcomeGrace() = %s and the delivery took %s — the publisher gives up while its holder is still sending", grace, took)
	}
	if got := mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1 — two pieces are on the person's screen", got)
	}
}

// silentAckConn writes and never answers, which is the shape of a peer that
// took the bytes and went quiet.
type silentAckConn struct {
	mu     sync.Mutex
	sender *wsSender
	writes int
}

func (c *silentAckConn) newSender() *wsSender {
	s := newWSSender(c, testLogger())
	c.mu.Lock()
	c.sender = s
	c.mu.Unlock()
	return s
}

func (c *silentAckConn) WriteMessage(int, []byte) error {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return nil
}
func (c *silentAckConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *silentAckConn) SetReadDeadline(time.Time) error   { return nil }
func (c *silentAckConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *silentAckConn) Close() error                      { return nil }
func (c *silentAckConn) attempts() int                     { c.mu.Lock(); defer c.mu.Unlock(); return c.writes }

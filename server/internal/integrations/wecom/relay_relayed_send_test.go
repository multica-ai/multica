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
	if env.Cmd == cmdSendMsg && body.MsgType == "markdown" {
		c.texts = append(c.texts, body.Markdown.Content)
	}
	s := c.sender
	c.mu.Unlock()
	if s != nil {
		s.routeResponse(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
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
var relayRetryConfig = RelayConfig{Shards: 1, LeaseSettle: 40 * time.Millisecond, RetryBackoff: 5 * time.Millisecond}

func newRelaySendRig(t *testing.T, failOn func(n int) bool) *relaySendRig {
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
	router := NewRelayOutbound(&fanoutRelay{}, nil, relayRetryConfig, testLogger())
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

package wecom

// metrics_test.go — the counters an operator reads.
//
// These assert the signal fires on the path it claims to describe. A counter
// wired to the wrong branch is worse than no counter: it reads as evidence the
// thing is being watched.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// countingMetrics records every call so a test can assert on the shape of what
// was reported rather than on a number in a registry.
type countingMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{counts: map[string]int{}}
}

func (m *countingMetrics) bump(k string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[k]++
}

func (m *countingMetrics) get(k string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[k]
}

func (m *countingMetrics) RecordConnectFailure()       { m.bump("connect_failure") }
func (m *countingMetrics) RecordAuthFailure()          { m.bump("auth_failure") }
func (m *countingMetrics) RecordCallbackQueued()       { m.bump("callback_queued") }
func (m *countingMetrics) RecordCallbackQueueBlocked() { m.bump("callback_blocked") }
func (m *countingMetrics) RecordStreamFinished()       { m.bump("stream_finished") }
func (m *countingMetrics) RecordStreamFellBack()       { m.bump("stream_fell_back") }
func (m *countingMetrics) RecordOutboundDelivered()    { m.bump("outbound_delivered") }
func (m *countingMetrics) RecordOutboundDropped(reason string) {
	m.bump("outbound_dropped")
	m.bump("outbound_dropped:" + reason)
}
func (m *countingMetrics) RecordOutboundSkipped(reason string) {
	m.bump("outbound_skipped")
	m.bump("outbound_skipped:" + reason)
}
func (m *countingMetrics) RecordAttachmentDelivered() { m.bump("attachment_delivered") }
func (m *countingMetrics) RecordAttachmentDropped(reason string) {
	m.bump("attachment_dropped")
	m.bump("attachment_dropped:" + reason)
}
func (m *countingMetrics) RecordAttachmentDeliveryShed() { m.bump("attachment_shed") }
func (m *countingMetrics) RecordRelayShed(kind string) {
	m.bump("relay_shed")
	m.bump("relay_shed:" + kind)
}
func (m *countingMetrics) RecordOutboundUnconfirmed(reason string) {
	m.bump("outbound_unconfirmed")
	m.bump("outbound_unconfirmed:" + reason)
}
func (m *countingMetrics) RecordAttachmentUnconfirmed(reason string) {
	m.bump("attachment_unconfirmed")
	m.bump("attachment_unconfirmed:" + reason)
}

var _ Metrics = (*countingMetrics)(nil)

// waitFor polls until cond holds or the budget runs out. The read loop runs on
// its own goroutine, so the counter it bumps is observed from here a moment
// after the event that caused it.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A handshake the server refused and a socket that never came up are the two
// causes an operator has to tell apart: the first needs somebody to fix the
// installation and will repeat identically on every backoff until they do, the
// second usually clears on its own. One "cannot connect" number cannot say
// which, so they are counted separately.
func TestARefusedHandshakeIsNotCountedAsAConnectFailure(t *testing.T) {
	t.Parallel()

	mx := newCountingMetrics()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: refusingConn(40001, "invalid credential")},
		wsURL:          "wss://example.test/ws",
		metrics:        mx,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("Connect succeeded against a server that refused the handshake")
	}
	// The counter and the error the Supervisor receives have to agree about
	// what happened; a counter that says "credentials" while the error says
	// something else is the drift this whole split exists to prevent.
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("Connect error = %v, want it to carry ErrCredentialsRejected", err)
	}
	if got := mx.get("auth_failure"); got != 1 {
		t.Fatalf("auth_failure = %d, want the rejection reported as a credential problem", got)
	}
	if got := mx.get("connect_failure"); got != 0 {
		t.Fatalf("connect_failure = %d for a socket that dialled and answered fine", got)
	}
}

// The other half of that verdict. 45009 is a throttle: the bot is fine, WeCom
// is rate-limiting, and classifySubscribeAck answers ErrCredentialsUnverifiable
// rather than ErrCredentialsRejected. The counter has to follow that answer
// instead of testing the errcode itself — an operator paged to rotate a
// perfectly good secret because of a throttle has been sent to cause a second
// outage. This is the case that would regress if somebody re-derived the
// classification beside the counter.
func TestAnUnverifiableHandshakeIsNotCountedAsACredentialFailure(t *testing.T) {
	t.Parallel()

	mx := newCountingMetrics()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: refusingConn(45009, "api freq out of limit")},
		wsURL:          "wss://example.test/ws",
		metrics:        mx,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("Connect succeeded against a server that refused the handshake")
	}
	if !errors.Is(err, ErrCredentialsUnverifiable) {
		t.Fatalf("Connect error = %v, want it to carry ErrCredentialsUnverifiable", err)
	}
	if got := mx.get("auth_failure"); got != 0 {
		t.Fatalf("auth_failure = %d for a throttle; nobody's credentials are wrong", got)
	}
	if got := mx.get("connect_failure"); got != 1 {
		t.Fatalf("connect_failure = %d, want the unverifiable ack counted on the side that clears on its own", got)
	}
}

// The other side of the same split: a dial that never lands is infrastructure,
// and must not show up as a credential problem — an operator who rotates a
// perfectly good secret because of it has been sent the wrong way.
func TestADialThatNeverLandsIsAConnectFailure(t *testing.T) {
	t.Parallel()

	mx := newCountingMetrics()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         failingDialer{err: errors.New("dial tcp: connection refused")},
		wsURL:          "wss://example.test/ws",
		metrics:        mx,
	}

	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect succeeded with a dialer that always fails")
	}
	if got := mx.get("connect_failure"); got != 1 {
		t.Fatalf("connect_failure = %d, want the failed dial reported", got)
	}
	if got := mx.get("auth_failure"); got != 0 {
		t.Fatalf("auth_failure = %d for a socket that never opened; nobody's credentials are wrong", got)
	}
}

// A handshake that is written and then never answered is the same class as a
// failed dial. It is the one the subscribeTimeout produces, and it is the one
// most likely to be misread as "the secret is wrong".
func TestAHandshakeThatIsNeverAnsweredIsAConnectFailure(t *testing.T) {
	t.Parallel()

	mx := newCountingMetrics()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: &silentSubscribeConn{}},
		wsURL:          "wss://example.test/ws",
		metrics:        mx,
	}

	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect succeeded against a server that never acked the handshake")
	}
	if got := mx.get("connect_failure"); got != 1 {
		t.Fatalf("connect_failure = %d, want the unanswered handshake reported", got)
	}
	if got := mx.get("auth_failure"); got != 0 {
		t.Fatalf("auth_failure = %d for a handshake the server never answered either way", got)
	}
}

// Backpressure is invisible from the outside: the read loop parks, the socket
// stops being drained, and the only outward sign is a connection WeCom
// eventually replaces. It has to be a number before it is an incident.
func TestAFullIngestQueueIsReportedAsBackpressure(t *testing.T) {
	t.Parallel()

	// One frame the worker holds, callbackQueueDepth more to fill the buffer,
	// and one that finds it full.
	total := callbackQueueDepth + 2
	frames := make([][]byte, 0, total)
	for i := 0; i < total; i++ {
		payload, err := json.Marshal(msgCallbackFrame(t, "text", "hello"))
		if err != nil {
			t.Fatalf("marshal frame: %v", err)
		}
		frames = append(frames, payload)
	}
	conn := &floodConn{
		frames:    frames,
		delivered: make(chan struct{}),
		unblock:   make(chan struct{}),
	}

	// The first callback's handler parks until the queue behind it is full.
	// Every later one returns immediately.
	var calls atomic.Int32
	release := make(chan struct{})

	mx := newCountingMetrics()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler: func(context.Context, channel.InboundMessage) error {
			if calls.Add(1) == 1 {
				<-release
			}
			return nil
		},
		dialer:  scriptedDialer{conn: conn},
		wsURL:   "wss://example.test/ws",
		metrics: mx,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Connect(ctx) }()

	waitFor(t, "the read loop to report a full ingest queue", func() bool {
		return mx.get("callback_blocked") >= 1
	})
	// The buffer really did fill, and the frames still outstanding are held by
	// a parked read loop rather than dropped. (Whether the worker had already
	// taken the first frame when the buffer filled is a race, so the count at
	// this instant is one of two values, not one.)
	queuedWhileBlocked := mx.get("callback_queued")
	if queuedWhileBlocked < callbackQueueDepth {
		t.Fatalf("callback_queued = %d when the queue reported full, want at least %d", queuedWhileBlocked, callbackQueueDepth)
	}
	if queuedWhileBlocked >= total {
		t.Fatalf("callback_queued = %d while the read loop is parked; it cannot have handed over all %d", queuedWhileBlocked, total)
	}

	close(release)
	waitFor(t, "the queue to drain", func() bool {
		return mx.get("callback_queued") == total
	})

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not return after ctx cancel")
	}
}

// A channel built without a sink must not be a nil-pointer dereference on the
// read loop. This is the deployment with /metrics turned off, which is the one
// least likely to be exercised anywhere else.
func TestAChannelWithNoSinkStillRuns(t *testing.T) {
	t.Parallel()

	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: refusingConn(40001, "invalid credential")},
		wsURL:          "wss://example.test/ws",
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect succeeded against a server that refused the handshake")
	}
}

// The factory is the other way a nil sink reaches the read loop, and it has to
// substitute the no-op rather than store the nil.
func TestTheFactorySubstitutesTheNoOpSink(t *testing.T) {
	t.Parallel()

	factory := newWecomFactory(ChannelDeps{Credentials: fixedCredentials{}})
	ch, err := factory(channel.Config{
		ID:      mustTestUUID(t),
		Raw:     []byte(`{"bot_id":"bot-1"}`),
		Handler: func(context.Context, channel.InboundMessage) error { return nil },
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	wc, ok := ch.(*wecomChannel)
	if !ok {
		t.Fatalf("factory returned %T, want *wecomChannel", ch)
	}
	if wc.metrics == nil {
		t.Fatal("factory stored a nil sink; the read loop would dereference it")
	}
	wc.metrics.RecordConnectFailure() // must not panic
}

// fixedCredentials is a CredentialsResolver that hands back whatever bot id the
// installation config carried, with no decryption.
type fixedCredentials struct{}

func (fixedCredentials) Credentials(inst Installation) (InstallationCredentials, error) {
	return InstallationCredentials{BotID: inst.BotID, Secret: "secret-1"}, nil
}

// rejectingSubscribeConn completes the handshake exchange and answers it with
// the errcode it was built with.
type rejectingSubscribeConn struct {
	errCode  int
	errMsg   string
	mu       sync.Mutex
	ack      []byte
	unblock  chan struct{}
	closeOne sync.Once
}

// refusingConn answers the handshake with one WeCom errcode. Which counter
// that lands on is classifySubscribeAck's decision, which is the whole point
// of driving these tests through a real code rather than a bool.
func refusingConn(code int, msg string) *rejectingSubscribeConn {
	return &rejectingSubscribeConn{errCode: code, errMsg: msg}
}

func (c *rejectingSubscribeConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err == nil && env.Cmd == cmdSubscribe {
		ack, _ := json.Marshal(frameEnvelope{
			Headers: frameHeaders{ReqID: env.Headers.ReqID},
			ErrCode: c.errCode,
			ErrMsg:  c.errMsg,
		})
		c.mu.Lock()
		c.ack = ack
		c.mu.Unlock()
	}
	return nil
}

func (c *rejectingSubscribeConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	if c.ack != nil {
		ack := c.ack
		c.ack = nil
		c.mu.Unlock()
		return websocket.TextMessage, ack, nil
	}
	c.mu.Unlock()
	return 0, nil, errors.New("closed")
}

func (c *rejectingSubscribeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *rejectingSubscribeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *rejectingSubscribeConn) Close() error {
	c.closeOne.Do(func() {
		if c.unblock != nil {
			close(c.unblock)
		}
	})
	return nil
}

// silentSubscribeConn accepts the handshake write and then never answers —
// the socket the subscribeTimeout exists for.
type silentSubscribeConn struct{}

func (silentSubscribeConn) WriteMessage(int, []byte) error { return nil }
func (silentSubscribeConn) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("i/o timeout")
}
func (silentSubscribeConn) SetReadDeadline(time.Time) error  { return nil }
func (silentSubscribeConn) SetWriteDeadline(time.Time) error { return nil }
func (silentSubscribeConn) Close() error                     { return nil }

// failingDialer never opens a socket.
type failingDialer struct{ err error }

func (d failingDialer) DialContext(context.Context, string, http.Header) (wsConn, *http.Response, error) {
	return nil, nil, d.err
}

// ---- the bubble ----
//
// The bubble is the one feature whose failure is invisible from the outside:
// the answer arrives either way, just as a separate message instead of in the
// bubble the question opened. Nobody files a ticket about that, so the ratio
// between these two counters is the only thing that can say the bubble has
// stopped working — after a WeCom-side change to the stream frame, say.

func TestAnAnswerThatLandsInTheBubbleIsCountedAsFinished(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	rig := newBubbleRig(t)
	rig.senders.WithMetrics(mx)

	rig.ran(t, "REQ-M1", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	if got := mx.get("stream_finished"); got != 1 {
		t.Fatalf("stream_finished = %d, want 1 — an answer sealed the bubble and nothing counted it, so a dashboard has no denominator to read fall-backs against", got)
	}
	if got := mx.get("stream_fell_back"); got != 0 {
		t.Fatalf("stream_fell_back = %d, want 0 — a bubble that worked was counted as a failure", got)
	}
}

func TestAnAnswerSentAsANewMessageIsCountedAsFallenBack(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	rig := newBubbleRig(t)
	rig.senders.WithMetrics(mx)
	rig.conn.refuseClosingCode = errcodeStreamExpired

	rig.ran(t, "REQ-M2", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	// The answer still reaches the user, which is why nobody reports this.
	if pushes := rig.conn.pushes(t); len(pushes) != 1 {
		t.Fatalf("the refused bubble delivered %d messages, want 1 — this test's premise is gone", len(pushes))
	}
	if got := mx.get("stream_fell_back"); got != 1 {
		t.Fatalf("stream_fell_back = %d, want 1 — every bubble on this deployment could be refusing its closing frame and nothing would say so", got)
	}
	if got := mx.get("stream_finished"); got != 0 {
		t.Fatalf("stream_finished = %d, want 0 — a refused closing frame was counted as a bubble that worked", got)
	}
}

// TestAFailureNoticeSentAsANewMessageIsCountedAsFallenBack — the same ending
// by the other closer.
//
// The answer is not the only thing that seals a bubble. task:failed,
// task:cancelled and the nine-minute guard all write a closing frame through
// TypingIndicatorManager.writeClosing, and all three fall back to a plain
// message when the server refuses it. Their successes have always been counted
// — sendersRegistry.stream records stream_finished for every closer — so the
// ratio the help text tells an operator to read had one feeder on the numerator
// and two on the denominator. A WeCom-side change that refused every closing
// frame would have moved the ratio for answers and left the failure and
// cancellation notices invisible, which is the case an operator most needs to
// see: those notices are the only thing that ever tells a user a run did not
// go through.
//
// REVERSE VERIFICATION: put RecordStreamFellBack back in Outbound.finishStream
// and take it out of sendersRegistry.stream. The sibling test above still
// passes — the answer path counts either way — and this one fails with
// stream_fell_back = 0. `go build`, `go vet` and every other test in the
// package stay green through that move: nothing else in the tree reads this
// counter.
func TestAFailureNoticeSentAsANewMessageIsCountedAsFallenBack(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	rig := newBubbleRig(t)
	rig.senders.WithMetrics(mx)
	rig.conn.refuseClosingCode = errcodeStreamExpired

	rig.ran(t, "REQ-M3", 1, "task-1")
	rig.failed(t, "task-1", false)

	// The user is still told the run failed, which is why nobody reports this.
	if pushes := rig.conn.pushes(t); len(pushes) != 1 {
		t.Fatalf("the refused bubble sent %d failure notices, want 1 — this test's premise is gone", len(pushes))
	}
	if got := mx.get("stream_fell_back"); got != 1 {
		t.Fatalf("stream_fell_back = %d, want 1 — every failure notice on this deployment could be "+
			"arriving as a separate message and the finished/fell_back ratio would look healthy", got)
	}
	if got := mx.get("stream_finished"); got != 0 {
		t.Fatalf("stream_finished = %d, want 0 — a refused closing frame was counted as a bubble that worked", got)
	}
}

// TestAFailureNoticeThatSealsTheBubbleIsCountedAsFinished is the other half of
// the pair, and the reason the one above matters: both closers already feed the
// denominator, so a fall-back counted at only one of them is not a gap in
// coverage but a ratio that reads backwards.
func TestAFailureNoticeThatSealsTheBubbleIsCountedAsFinished(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	rig := newBubbleRig(t)
	rig.senders.WithMetrics(mx)

	rig.ran(t, "REQ-M4", 1, "task-1")
	rig.failed(t, "task-1", false)

	if got := mx.get("stream_finished"); got != 1 {
		t.Fatalf("stream_finished = %d, want 1", got)
	}
	if got := mx.get("stream_fell_back"); got != 0 {
		t.Fatalf("stream_fell_back = %d, want 0 — a notice that sealed its bubble was counted as a fall-back", got)
	}
}

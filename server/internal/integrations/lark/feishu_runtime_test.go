package lark

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// These tests pin the inbound-runtime invariants that moved out of the
// former lark.Hub.handleEvent / scheduleReply when MUL-3620 split the
// channel-agnostic engine.Supervisor (connection lifecycle) from the Feishu
// reply logic (FeishuRuntime). They are the relocated hub_test.go reply
// coverage, retargeted at FeishuRuntime.

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// slowReplier blocks Reply for the configured duration unless ctx fires
// first, proving the reply-off-critical-path invariant: a Reply exceeding
// ReplyTimeout MUST get its ctx cancelled instead of running unbounded.
type slowReplier struct {
	delay      time.Duration
	startCh    chan struct{}
	finishCh   chan struct{}
	mu         sync.Mutex
	callCount  int
	lastCtxErr error
}

func newSlowReplier(delay time.Duration) *slowReplier {
	return &slowReplier{
		delay:    delay,
		startCh:  make(chan struct{}, 16),
		finishCh: make(chan struct{}, 16),
	}
}

func (s *slowReplier) Reply(ctx context.Context, _ Installation, _ InboundMessage, _ DispatchResult) {
	s.mu.Lock()
	s.callCount++
	s.mu.Unlock()
	select {
	case s.startCh <- struct{}{}:
	default:
	}
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
	}
	s.mu.Lock()
	s.lastCtxErr = ctx.Err()
	s.mu.Unlock()
	select {
	case s.finishCh <- struct{}{}:
	default:
	}
}

func (s *slowReplier) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

func (s *slowReplier) ctxErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCtxErr
}

// drainWithin runs rt.Drain in a goroutine and reports whether it completed
// within d.
func drainWithin(rt *FeishuRuntime, d time.Duration) bool {
	done := make(chan struct{})
	go func() { rt.Drain(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestFeishuRuntimeScheduleReplyReturnsImmediately(t *testing.T) {
	t.Parallel()
	rep := newSlowReplier(10 * time.Second)
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{ReplyTimeout: 100 * time.Millisecond, Logger: newDiscardLogger()})
	rt.SetOutcomeReplier(rep)

	start := time.Now()
	rt.scheduleReply(Installation{}, InboundMessage{EventID: "e1"}, DispatchResult{Outcome: OutcomeNeedsBinding})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("scheduleReply took %s; ACK critical path would be blocked by outbound HTTP", elapsed)
	}

	select {
	case <-rep.startCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("detached replier never ran")
	}

	if !drainWithin(rt, 1*time.Second) {
		t.Fatal("reply goroutine did not exit after ReplyTimeout fired")
	}
	if !errors.Is(rep.ctxErr(), context.DeadlineExceeded) {
		t.Fatalf("replier ctx.Err() = %v; want DeadlineExceeded", rep.ctxErr())
	}
}

func TestFeishuRuntimeReplyTimeoutCancelsHungReplier(t *testing.T) {
	t.Parallel()
	timeout := 80 * time.Millisecond
	rep := newSlowReplier(10 * time.Second)
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{ReplyTimeout: timeout, Logger: newDiscardLogger()})
	rt.SetOutcomeReplier(rep)

	start := time.Now()
	rt.scheduleReply(Installation{}, InboundMessage{EventID: "e2"}, DispatchResult{Outcome: OutcomeAgentOffline})

	select {
	case <-rep.finishCh:
	case <-time.After(1 * time.Second):
		t.Fatal("replier never exited; ReplyTimeout did not fire")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("replier exit took %s; expected ≈ %s", elapsed, timeout)
	}
	if !errors.Is(rep.ctxErr(), context.DeadlineExceeded) {
		t.Fatalf("replier should observe DeadlineExceeded; got %v", rep.ctxErr())
	}
	rt.Drain()
}

func TestFeishuRuntimeDrainsInFlightReplies(t *testing.T) {
	t.Parallel()
	rep := newSlowReplier(30 * time.Millisecond) // shorter than ReplyTimeout
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{ReplyTimeout: 1 * time.Second, Logger: newDiscardLogger()})
	rt.SetOutcomeReplier(rep)

	rt.scheduleReply(Installation{}, InboundMessage{EventID: "e3"}, DispatchResult{Outcome: OutcomeNeedsBinding})

	start := time.Now()
	rt.Drain()
	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond {
		t.Fatalf("Drain returned in %s; should have blocked until the reply completed", elapsed)
	}
	if rep.calls() != 1 {
		t.Fatalf("reply call count = %d; want 1", rep.calls())
	}
	if rep.ctxErr() != nil {
		t.Errorf("reply ctxErr = %v; want nil (slept normally)", rep.ctxErr())
	}
}

func TestFeishuRuntimeNoopReplierInlineNoGoroutine(t *testing.T) {
	t.Parallel()
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{Logger: newDiscardLogger()})
	// replier defaults to noop -> inline, no goroutine. 1000 calls then a
	// Drain that must return immediately (nothing to join).
	for i := 0; i < 1000; i++ {
		rt.scheduleReply(Installation{}, InboundMessage{EventID: "e"}, DispatchResult{Outcome: OutcomeNeedsBinding})
	}
	if !drainWithin(rt, 100*time.Millisecond) {
		t.Fatal("Drain should return immediately when no goroutine replies were scheduled")
	}
}

func TestFeishuRuntimeReplyTimeoutDefaultIsUnder3s(t *testing.T) {
	t.Parallel()
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{})
	if rt.replyTimeout <= 0 {
		t.Fatalf("ReplyTimeout default must be > 0; got %s", rt.replyTimeout)
	}
	if rt.replyTimeout >= 3*time.Second {
		t.Fatalf("ReplyTimeout default %s is too close to Lark's 3s ACK deadline", rt.replyTimeout)
	}
}

func TestFeishuRuntimeHandleEventSurfacesDispatcherMisconfig(t *testing.T) {
	t.Parallel()
	// No dispatcher wired -> handleEvent surfaces ErrDispatcherNotConfigured
	// to the connector instead of silently dropping the event.
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{Logger: newDiscardLogger()})
	res, err := rt.handleEvent(context.Background(), Installation{}, InboundMessage{EventID: "evt-1"})
	if !errors.Is(err, ErrDispatcherNotConfigured) {
		t.Fatalf("handleEvent should surface dispatcher misconfig; got %v", err)
	}
	if res.Outcome != "" {
		t.Fatalf("handleEvent should not invent an outcome on dispatcher error; got %q", res.Outcome)
	}
}

func TestFeishuRuntimeDefaultsToNoopReplier(t *testing.T) {
	t.Parallel()
	// A runtime whose caller never calls SetOutcomeReplier still has a
	// non-nil replier, so scheduleReply never nil-panics (the "no nil
	// replier crash" contract, formerly on the Hub).
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{})
	if rt.replier == nil {
		t.Fatal("FeishuRuntime must default to a non-nil (noop) replier")
	}
}

// TestFeishuRuntimeACKNotBlockedByOutboundReply is the integrated invariant:
// even when the OutcomeReplier hangs far longer than Lark's 3s ACK deadline,
// the connector's data-frame ACK still lands promptly. Driven through the
// real WSLongConnConnector with an emit that delegates to the runtime's
// reply scheduling (the seam the feishuChannel uses in production).
func TestFeishuRuntimeACKNotBlockedByOutboundReply(t *testing.T) {
	t.Parallel()

	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func(payload []byte, _ Installation) (InboundMessage, bool, error) {
		return InboundMessage{EventID: string(payload)}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour) // disable ping

	rep := newSlowReplier(5 * time.Second)
	rt := NewFeishuRuntime(nil, FeishuRuntimeConfig{ReplyTimeout: 2500 * time.Millisecond, Logger: newDiscardLogger()})
	rt.SetOutcomeReplier(rep)

	emit := func(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
		// Stand in for the dispatcher's "binding-needed" verdict without the
		// full Dispatcher; the async reply scheduling is what's under test.
		res := DispatchResult{Outcome: OutcomeNeedsBinding, SenderOpenID: "ou_user_42"}
		rt.scheduleReply(Installation{}, msg, res)
		return res, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, Installation{AppID: "test_app"}, emit) }()

	start := time.Now()
	pushDataFrame(conn, []byte("evt-binding"), "om-binding")

	if !waitFor(500*time.Millisecond, func() bool {
		for _, w := range conn.snapshot() {
			f, err := UnmarshalFrame(w)
			if err != nil {
				continue
			}
			if f.Method == FrameMethodData && bytes.Contains(f.Payload, []byte(`"code":200`)) {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("data-frame ACK did not land within 500ms; outbound reply blocked the critical path (replier ran? %v)", rep.calls() == 1)
	}
	if ackLatency := time.Since(start); ackLatency >= 3*time.Second {
		t.Fatalf("ACK landed in %s, past Lark's 3s deadline", ackLatency)
	}

	select {
	case <-rep.startCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("replier never ran; the reply path is silently broken")
	}

	cancel()
	<-done
	rt.Drain()
}

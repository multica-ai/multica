package feishu_test

// TC-adapt-3 (PRD AC2.1): the feishu adapter must survive a Disconnect
// and accept a subsequent Connect, replaying the upstream SDK's buffered
// events through the inbound dedup pipeline so each logical event is
// processed exactly once.
//
// The whole-system property under test is:
//
//	1. Connect → push N distinct events → consume them all
//	2. Disconnect (simulating the 30s outage)
//	3. Connect again
//	4. Push the SAME N events (the SDK's replay buffer doing its job)
//	5. The dedup store sees 2N TryRecordInboundEvent calls but only
//	   inserts N rows — so a downstream IssueFacade-style sink sees
//	   each event exactly once, not twice.
//
// The test wires a real inbound.Pipeline (NormalizeStep → DedupStep →
// terminal sink spy) on top of the real feishu.Adapter so a reconnect
// regression that re-creates the dedup table or breaks the (provider,
// event_id) key would also be caught here.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// Reconnectable fake — variant of fakeFeishuClient that supports
// Stop()-then-Start() re-entry (the production WebSocket SDK does this
// natively; the fake in adapter_test.go uses sync.Once which would panic
// on second Stop). We isolate the variant in this file so the existing
// adapter_test.go doubles stay small.
// ---------------------------------------------------------------------------

type reconnectableFakeClient struct {
	botUserID string

	mu     sync.Mutex
	events chan feishu.RawEvent
	// closed tracks whether the current events chan has been Stop-closed
	// so a subsequent Start can rebuild a fresh chan rather than reuse
	// the closed one.
	closed bool
}

func newReconnectableFakeClient(botID string) *reconnectableFakeClient {
	return &reconnectableFakeClient{
		botUserID: botID,
		events:    make(chan feishu.RawEvent, 16),
	}
}

func (f *reconnectableFakeClient) BotUserID() string { return f.botUserID }

func (f *reconnectableFakeClient) Subscribe() <-chan feishu.RawEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events
}

func (f *reconnectableFakeClient) Start(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		// Re-arm: build a fresh channel for the new session.
		f.events = make(chan feishu.RawEvent, 16)
		f.closed = false
	}
	return nil
}

func (f *reconnectableFakeClient) Stop(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		close(f.events)
		f.closed = true
	}
	return nil
}

func (f *reconnectableFakeClient) SendMessage(context.Context, feishu.SendRequest) (feishu.SendResponse, error) {
	return feishu.SendResponse{MessageID: "om_test"}, nil
}

func (f *reconnectableFakeClient) GetChatInfo(_ context.Context, chatID string) (feishu.ChatInfoResponse, error) {
	return feishu.ChatInfoResponse{ID: chatID, Name: "stub"}, nil
}

func (f *reconnectableFakeClient) GetUserInfo(_ context.Context, userID string) (feishu.UserInfoResponse, error) {
	return feishu.UserInfoResponse{OpenID: userID, Name: "stub"}, nil
}

// pushReplay sends N events, optionally waiting for the subscriber to
// drain so a buffered chan doesn't backpressure the producer.
func (f *reconnectableFakeClient) pushReplay(t *testing.T, evs []feishu.RawEvent) {
	t.Helper()
	f.mu.Lock()
	ch := f.events
	closed := f.closed
	f.mu.Unlock()
	if closed {
		t.Fatalf("pushReplay: client is stopped; restart before pushing")
	}
	for _, ev := range evs {
		select {
		case ch <- ev:
		case <-time.After(2 * time.Second):
			t.Fatalf("pushReplay: events chan blocked for >2s on %s", ev.EventID)
		}
	}
}

// ---------------------------------------------------------------------------
// In-memory dedup store mirroring the (provider, event_id) PK contract
// of channel_inbound_event_dedup. Implements inbound.DedupStore so we can
// wire the real DedupStep without dragging in a Postgres dependency.
// ---------------------------------------------------------------------------

type memoryDedupStore struct {
	mu   sync.Mutex
	seen map[string]bool // key = provider + "|" + eventID
}

func newMemoryDedupStore() *memoryDedupStore {
	return &memoryDedupStore{seen: map[string]bool{}}
}

func (m *memoryDedupStore) TryRecordInboundEvent(_ context.Context, provider, _ string, eventID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := provider + "|" + eventID
	if m.seen[key] {
		return false, nil
	}
	m.seen[key] = true
	return true, nil
}

// ---------------------------------------------------------------------------
// Sink Step — terminal counter that records every event the pipeline
// reaches. Stand-in for the IssueFacade.CreateIssue branch the issue
// spec calls out: "断言 IssueFacade.CreateIssue 只被调 5 次（不是 10 次）".
// ---------------------------------------------------------------------------

type sinkStep struct {
	mu    sync.Mutex
	count int
	ids   []string
}

func newSinkStep() *sinkStep { return &sinkStep{} }

// Name uses a pointer receiver to avoid the race detector flagging a
// value-copy of a struct that contains sync.Mutex while another
// goroutine holds that mutex.
func (*sinkStep) Name() string { return "sink" }
func (s *sinkStep) snapshot() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.ids))
	copy(out, s.ids)
	return s.count, out
}
func (s *sinkStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	s.ids = append(s.ids, evt.EventID)
	return evt, inbound.DecisionContinue, nil
}

// makeRecvEvent fabricates an im.message.receive_v1 RawEvent with the
// given event_id. The body is the smallest payload the normaliser will
// accept as a text message.
func makeRecvEvent(eventID string) feishu.RawEvent {
	body := fmt.Sprintf(`{
        "schema": "2.0",
        "header": {"event_id": %q, "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_%s",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"hi\"}",
                "mentions": []
            }
        }
    }`, eventID, eventID)
	return feishu.RawEvent{
		EventID:   eventID,
		EventType: "im.message.receive_v1",
		Payload:   json.RawMessage(body),
	}
}

// ---------------------------------------------------------------------------
// TC-adapt-3 — Disconnect / Connect / replay survives via dedup.
// ---------------------------------------------------------------------------

func TestAdapter_Reconnect_DedupSwallowsReplay(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newReconnectableFakeClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	dedup := newMemoryDedupStore()
	sink := newSinkStep()
	pipeline := inbound.NewPipeline(
		inbound.NewNormalizeStep(),
		inbound.NewDedupStep(dedup),
		sink,
	)

	// Forward Events() through the pipeline in a goroutine. A real
	// wiring site would do the same — see cmd/server/main.go.
	pipeCtx, pipeCancel := context.WithCancel(context.Background())
	defer pipeCancel()

	consumerStarted := make(chan struct{})
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		close(consumerStarted)
		for evt := range adapter.Events() {
			_, _ = pipeline.Run(pipeCtx, evt)
		}
	}()
	<-consumerStarted

	// --- Phase 1: first connection, push 5 distinct events ---
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}

	original := make([]feishu.RawEvent, 5)
	for i := range original {
		original[i] = makeRecvEvent(fmt.Sprintf("evt-%d", i+1))
	}
	fake.pushReplay(t, original)

	// Wait for the sink to observe all 5.
	waitForCount(t, sink, 5, 2*time.Second)

	// --- Phase 2: simulate the 30s outage with Disconnect ---
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	// Re-attach the consumer to the new Events() the reconnect produces.
	// The previous goroutine returned when the old chan closed, so spawn
	// a fresh consumer for phase 3.
	<-consumerDone

	// --- Phase 3: reconnect; SDK replays the buffered 5 events ---
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect after Disconnect: %v", err)
	}

	consumer2Done := make(chan struct{})
	go func() {
		defer close(consumer2Done)
		for evt := range adapter.Events() {
			_, _ = pipeline.Run(pipeCtx, evt)
		}
	}()

	fake.pushReplay(t, original) // identical event ids — SDK replay

	// Give the pipeline time to consume and dedup.
	waitForStable(t, sink, 5, 500*time.Millisecond)

	// --- Phase 4: assertions ---
	count, ids := sink.snapshot()
	if count != 5 {
		t.Fatalf("sink saw %d events (ids=%v); want 5 (dedup must drop the replay)", count, ids)
	}

	// Every original id should appear exactly once in the sink.
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	for _, ev := range original {
		if seen[ev.EventID] != 1 {
			t.Errorf("event %q reached sink %d times, want 1", ev.EventID, seen[ev.EventID])
		}
	}

	// Cleanup
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("final Disconnect: %v", err)
	}
	<-consumer2Done
}

// ---------------------------------------------------------------------------
// Reconnect smoke: two Disconnect/Connect cycles do not panic on
// "send on closed channel" (the trap the lifecycle refactor must avoid).
// ---------------------------------------------------------------------------

func TestAdapter_Reconnect_TwoCyclesNoPanic(t *testing.T) {
	t.Parallel()

	fake := newReconnectableFakeClient("ou_bot")
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	for cycle := 0; cycle < 2; cycle++ {
		if err := adapter.Connect(context.Background()); err != nil {
			t.Fatalf("cycle %d Connect: %v", cycle, err)
		}
		drainStarted := make(chan struct{})
		drainDone := make(chan struct{})
		go func() {
			defer close(drainDone)
			close(drainStarted)
			for range adapter.Events() {
			}
		}()
		<-drainStarted
		fake.pushReplay(t, []feishu.RawEvent{makeRecvEvent(fmt.Sprintf("c%d-evt", cycle))})
		// give the pump a brief moment to forward
		time.Sleep(50 * time.Millisecond)
		if err := adapter.Disconnect(context.Background()); err != nil {
			t.Fatalf("cycle %d Disconnect: %v", cycle, err)
		}
		<-drainDone
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func waitForCount(t *testing.T, s *sinkStep, want int, dur time.Duration) {
	t.Helper()
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if c, _ := s.snapshot(); c >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, ids := s.snapshot()
	t.Fatalf("waited %s for sink count >= %d, got %d (ids=%v)", dur, want, c, ids)
}

// waitForStable asserts the sink count remains == want for the entire
// duration. Used to detect "duplicate replay was actually delivered"
// regressions where dedup is silently bypassed.
func waitForStable(t *testing.T, s *sinkStep, want int, dur time.Duration) {
	t.Helper()
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		c, ids := s.snapshot()
		if c != want {
			t.Fatalf("sink count drifted from %d to %d (ids=%v) — dedup likely bypassed", want, c, ids)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

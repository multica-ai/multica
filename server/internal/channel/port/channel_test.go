package port_test

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// mockChannel is a minimal in-test implementation of port.Channel used by
// TC-port-1, TC-port-2, TC-port-3. It is intentionally local to the test file
// so it cannot leak into production binaries (DESIGN §3.2: no production
// shortcut paths through test-only doubles).
type mockChannel struct {
	name   string
	events chan port.InboundEvent
	closed bool
}

func newMockChannel(name string) *mockChannel {
	return &mockChannel{
		name:   name,
		events: make(chan port.InboundEvent, 1),
	}
}

func (m *mockChannel) Name() string                                  { return m.name }
func (m *mockChannel) Connect(ctx context.Context) error             { return nil }
func (m *mockChannel) Events() <-chan port.InboundEvent              { return m.events }
func (m *mockChannel) GetChatInfo(ctx context.Context, chatID string) (port.ChatInfo, error) {
	return port.ChatInfo{}, nil
}
func (m *mockChannel) GetUserInfo(ctx context.Context, userID string) (port.UserInfo, error) {
	return port.UserInfo{}, nil
}
func (m *mockChannel) Send(ctx context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	return port.SendResult{}, nil
}
func (m *mockChannel) SendCard(ctx context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	return port.SendResult{}, nil
}
func (m *mockChannel) Disconnect(ctx context.Context) error {
	if !m.closed {
		close(m.events)
		m.closed = true
	}
	return nil
}

// TC-port-3 · Events() channel close semantics.
//
// After Disconnect returns, the channel returned by Events() must be closed so
// downstream `for range` consumers terminate cleanly. Per TestCase §1: a 1s
// timeout without close should fail the test (prevents the bug where adapters
// just return nil from Disconnect and leave goroutines pinned forever).
func TestPortChannel_EventsClosedAfterDisconnect(t *testing.T) {
	t.Parallel()

	mc := newMockChannel("mock")
	ch := mc.Events()

	if err := mc.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected Events() channel to be closed after Disconnect, but receive succeeded with ok=true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Events() channel was not closed within 1s after Disconnect")
	}
}

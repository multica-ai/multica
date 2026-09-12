package dingtalk

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestOutboundArchiveClearsPendingReceiptsOnlyForItsAgent(t *testing.T) {
	n, actions := newTestAckWithMessageIDs(time.Now)
	inst := engine.ResolvedInstallation{ID: sessionUUID(90), AgentID: sessionUUID(80)}
	other := engine.ResolvedInstallation{ID: sessionUUID(91), AgentID: sessionUUID(81)}
	sid := sessionUUID(92)
	add := func(inst engine.ResolvedInstallation, name string, input byte, session byte) {
		n.client.rememberReplySource(inst.ID, sessionUUID(input), groupReactionMessage(name))
		n.OnIngested(context.Background(), inst, groupReactionMessage(name), sessionUUID(session))
	}
	add(inst, "pending", 1, 92)
	add(inst, "another-session", 2, 93)
	add(other, "other-agent", 3, 94)
	// There is no task: the subscriber must not query one to clean up archive.
	o := NewOutbound(noDeliveryOutboundQueries{}, nil, n.client, n, nil)
	bus := events.New()
	o.Register(bus)
	e := events.Event{Type: protocol.EventAgentArchived, Payload: map[string]any{"agent": map[string]any{"id": util.UUIDToString(inst.AgentID)}}}
	bus.Publish(e)
	bus.Publish(e)
	// A repeated hook for an archived input stays retired, even after restore.
	bus.Publish(events.Event{Type: protocol.EventAgentRestored, Payload: e.Payload})
	n.OnIngested(context.Background(), inst, groupReactionMessage("pending"), sid)
	add(inst, "after-restore", 4, 92)
	want := []string{"add:pending:" + emotionAcknowledged, "add:another-session:" + emotionAcknowledged, "add:other-agent:" + emotionAcknowledged}
	if !slices.Equal((*actions)[:3], want) {
		t.Fatalf("fixture: %v", *actions)
	}
	recalls := append([]string(nil), (*actions)[3:len(*actions)-1]...)
	slices.Sort(recalls)
	if !slices.Equal(recalls, []string{"recall:another-session:" + emotionAcknowledged, "recall:pending:" + emotionAcknowledged}) || (*actions)[len(*actions)-1] != "add:after-restore:"+emotionAcknowledged {
		t.Fatalf("archive crossed agent/input boundary: %v", *actions)
	}
	if len(n.active) != 2 {
		t.Fatalf("expected other agent and restored input to remain: %v", n.active)
	}
}

func TestOutboundArchiveRecallsAddThatFinishesAfterArchive(t *testing.T) {
	n := NewAckNotifier(nil, nil, nil, nil)
	inst := engine.ResolvedInstallation{ID: sessionUUID(90), AgentID: sessionUUID(80)}
	msg := groupReactionMessage("pending")
	n.client.rememberReplySource(inst.ID, sessionUUID(1), msg)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	visible := false
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, name string, recall bool) error {
		if name != emotionAcknowledged {
			t.Error("archive marked input done")
		}
		if !recall {
			close(started)
			<-release
		}
		mu.Lock()
		visible = !recall
		mu.Unlock()
		return nil
	}
	go func() { defer close(done); n.OnIngested(context.Background(), inst, msg, sessionUUID(92)) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("receipt add never started")
	}
	NewOutbound(nil, nil, n.client, n, nil).handleAgentArchived(events.Event{Payload: map[string]any{"agent": map[string]any{"id": util.UUIDToString(inst.AgentID)}}})
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receipt add never finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if visible || len(n.active) != 0 {
		t.Fatalf("late provider add survived archive: visible=%v active=%v", visible, n.active)
	}
}

func TestOutboundArchiveIgnoresInvalidPayload(t *testing.T) {
	for _, payload := range []any{nil, make(chan int), map[string]any{"agent": "wrong type"}, map[string]any{"agent": map[string]any{"id": "not-a-uuid"}}} {
		n, actions := newTestAckWithMessageIDs(time.Now)
		n.OnIngested(context.Background(), engine.ResolvedInstallation{AgentID: sessionUUID(80)}, groupReactionMessage("pending"), sessionUUID(92))
		NewOutbound(nil, nil, n.client, n, nil).handleAgentArchived(events.Event{Payload: payload})
		if !slices.Equal(*actions, []string{"add:pending:" + emotionAcknowledged}) {
			t.Fatalf("invalid archive cleared receipt: %v", *actions)
		}
	}
}

func TestOutboundArchiveWithoutNotifierOrAgentIsNoOp(t *testing.T) {
	NewOutbound(nil, nil, nil, nil, nil).handleAgentArchived(events.Event{})
	n, actions := newTestAckWithMessageIDs(time.Now)
	n.OnIngested(context.Background(), engine.ResolvedInstallation{}, groupReactionMessage("pending"), sessionUUID(92))
	n.onAgentArchived(context.Background(), engine.ResolvedInstallation{}.AgentID)
	if !slices.Equal(*actions, []string{"add:pending:" + emotionAcknowledged}) {
		t.Fatalf("missing archive owner cleared receipt: %v", *actions)
	}
}

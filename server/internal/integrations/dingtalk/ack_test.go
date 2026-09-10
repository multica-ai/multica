package dingtalk

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func sessionUUID(b byte) pgtype.UUID {
	u := pgtype.UUID{Valid: true}
	u.Bytes[0] = b
	return u
}
func groupReactionMessage(id string) channel.InboundMessage {
	return channel.InboundMessage{MessageID: id, Source: channel.Source{ChatID: "cid-1", ChatType: channel.ChatTypeGroup}}
}
func newTestAck(_ func() time.Time) (*ackNotifier, *[]string) {
	n := NewAckNotifier(nil, nil, nil, nil)
	var actions []string
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, name string, recall bool) error {
		verb := "add:"
		if recall {
			verb = "recall:"
		}
		actions = append(actions, verb+name)
		return nil
	}
	return n, &actions
}
func newTestAckWithMessageIDs(_ func() time.Time) (*ackNotifier, *[]string) {
	n := NewAckNotifier(nil, nil, nil, nil)
	var actions []string
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, name string, recall bool) error {
		verb := "add:"
		if recall {
			verb = "recall:"
		}
		actions = append(actions, verb+msg.MessageID+":"+name)
		return nil
	}
	return n, &actions
}
func TestAckNotifierClearsSessionWithoutMarkingOtherInputsDone(t *testing.T) {
	n, actions := newTestAckWithMessageIDs(time.Now)
	ctx := context.Background()
	sid := sessionUUID(1)
	inst := engine.ResolvedInstallation{ID: sessionUUID(9)}
	n.OnIngested(ctx, inst, groupReactionMessage("a"), sid)
	n.OnIngested(ctx, inst, groupReactionMessage("b"), sid)
	n.OnIngested(ctx, inst, groupReactionMessage("other"), sessionUUID(2))
	n.OnSettled(ctx, sid)
	n.client.rememberReplySource(inst.ID, sessionUUID(10), groupReactionMessage("a"))
	n.OnReplyDelivered(ctx, inst, sessionUUID(10))
	want := []string{"add:a:收到", "add:b:收到", "add:other:收到", "recall:a:收到", "recall:b:收到", "add:a:Done"}
	if !slices.Equal(*actions, want) {
		t.Fatalf("actions=%v want=%v", *actions, want)
	}
}
func TestAckNotifierDuplicateIngestDoesNotDuplicateReaction(t *testing.T) {
	n, actions := newTestAck(time.Now)
	ctx := context.Background()
	sid := sessionUUID(1)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, groupReactionMessage("a"), sid)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, groupReactionMessage("a"), sid)
	if len(*actions) != 1 {
		t.Fatalf("duplicate reactions: %v", *actions)
	}
}
func TestAckNotifierFailedAddStillAttemptsBoundedRecall(t *testing.T) {
	n := NewAckNotifier(nil, nil, nil, nil)
	var calls int
	n.sendReaction = func(ctx context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, _ string, recall bool) error {
		calls++
		if recall && ctx.Err() != nil {
			t.Error("cleanup inherited cancelled context")
		}
		return errors.New("uncertain provider result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	sid := sessionUUID(1)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, groupReactionMessage("a"), sid)
	cancel()
	n.OnSettled(ctx, sid)
	n.OnSettled(context.Background(), sid)
	if calls != 2 || len(n.active) != 0 {
		t.Fatalf("calls=%d active=%d", calls, len(n.active))
	}
}
func TestAckNotifierRecallsAddThatFinishesAfterClear(t *testing.T) {
	n := NewAckNotifier(nil, nil, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	var actions []string
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, _ string, recall bool) error {
		if !recall {
			close(started)
			<-release
		}
		mu.Lock()
		defer mu.Unlock()
		if recall {
			actions = append(actions, "recall")
		} else {
			actions = append(actions, "add")
		}
		return nil
	}
	sid := sessionUUID(1)
	go func() {
		defer close(done)
		n.OnIngested(context.Background(), engine.ResolvedInstallation{}, groupReactionMessage("a"), sid)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("add did not start")
	}
	n.OnSettled(context.Background(), sid)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("add did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(actions, []string{"recall", "add", "recall"}) {
		t.Fatalf("actions=%v", actions)
	}
}
func TestAckNotifierInvalidCoordinatesDoNothing(t *testing.T) {
	n, actions := newTestAck(time.Now)
	n.OnIngested(context.Background(), engine.ResolvedInstallation{}, groupReactionMessage("a"), pgtype.UUID{})
	n.OnIngested(context.Background(), engine.ResolvedInstallation{}, groupReactionMessage(""), sessionUUID(1))
	n.OnIngested(context.Background(), engine.ResolvedInstallation{}, channel.InboundMessage{MessageID: "a"}, sessionUUID(1))
	n.OnReplyDelivered(context.Background(), engine.ResolvedInstallation{}, pgtype.UUID{})
	if len(*actions) != 0 {
		t.Fatalf("invalid coordinates sent: %v", *actions)
	}
}

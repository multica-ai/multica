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
<<<<<<< HEAD
	inst := engine.ResolvedInstallation{ID: sessionUUID(9)}
	n.OnIngested(ctx, inst, groupReactionMessage("a"), sid)
	n.OnIngested(ctx, inst, groupReactionMessage("b"), sid)
	n.OnIngested(ctx, inst, groupReactionMessage("other"), sessionUUID(2))
	n.OnSettled(ctx, sid)
	n.client.rememberReplySource(inst.ID, sessionUUID(10), sid, groupReactionMessage("a"))
	n.OnReplyDelivered(ctx, inst, sessionUUID(10))
	want := []string{"add:a:收到", "add:b:收到", "add:other:收到", "recall:a:收到", "recall:b:收到", "add:a:Done"}
	if !slices.Equal(*actions, want) {
		t.Fatalf("actions=%v want=%v", *actions, want)
=======
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 1 {
		t.Fatalf("a burst within the window must coalesce to one ack, got %d", len(*sent))
	}
	if (*sent)[0] != ackProcessingText {
		t.Errorf("ack text = %q, want %q", (*sent)[0], ackProcessingText)
	}

	cur = base.Add(6 * time.Second)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("a message after the window must re-ack, got %d", len(*sent))
>>>>>>> 8c75d38c4 (feat(wecom): bind a bubble to its run off the bus, not off the engine)
	}
}
func TestAckNotifierDuplicateIngestDoesNotDuplicateReaction(t *testing.T) {
	n, actions := newTestAck(time.Now)
	ctx := context.Background()
<<<<<<< HEAD
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
=======

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnSettled(ctx, sid)
	// Even within the window, a settled session acks its next turn immediately.
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("OnSettled must reset dedup so the next turn re-acks, got %d", len(*sent))
	}
}

func TestAckNotifier_DistinctSessionsAckIndependently(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, sent := newTestAck(func() time.Time { return cur })
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(3))
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(4))
	if len(*sent) != 2 {
		t.Fatalf("distinct sessions must each ack, got %d", len(*sent))
>>>>>>> 8c75d38c4 (feat(wecom): bind a bubble to its run off the bus, not off the engine)
	}
}

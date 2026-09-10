package dingtalk

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type ackInputQueries struct {
	rows map[pgtype.UUID]db.ChatMessage
	read func(pgtype.UUID)
	err  error
}

func (q *ackInputQueries) GetChatMessage(_ context.Context, id pgtype.UUID) (db.ChatMessage, error) {
	if q.read != nil {
		q.read(id)
	}
	if q.err != nil {
		return db.ChatMessage{}, q.err
	}
	row, ok := q.rows[id]
	if !ok {
		return row, pgx.ErrNoRows
	}
	return row, nil
}

func TestAckBatchUsesSealedOwnershipAndInputOrder(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		sealed, fresh, reversed bool
		want                    []string
	}{
		{"pending batch", false, false, false, []string{"add:a", "recall:a", "add:b"}},
		{"same sealed batch", true, false, false, []string{"add:a", "recall:a", "add:b"}},
		{"late older hook", false, false, true, []string{"add:b"}},
		{"new context", false, true, false, []string{"add:a", "add:b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
			q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
			n := NewAckNotifier(nil, nil, nil, q)
			var actions []string
			n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
				verb := "add:"
				if recall {
					verb = "recall:"
				}
				actions = append(actions, verb+msg.MessageID)
				return nil
			}
			for i, name := range []string{"a", "b"} {
				id := sessionUUID(byte(92 + i))
				row := db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true, ChannelContextRevision: pgtype.Int8{Int64: 1, Valid: true}, CreatedAt: pgtype.Timestamptz{Time: time.Unix(int64(i), 0), Valid: true}}
				if tc.sealed {
					row.TaskID = sessionUUID(94)
				}
				if tc.fresh && i == 1 {
					row.ChannelContextRevision.Int64 = 2
				}
				q.rows[id] = row
				n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
			}
			order := []string{"a", "b"}
			if tc.reversed {
				slices.Reverse(order)
			}
			for _, name := range order {
				n.OnIngested(ctx, inst, groupReactionMessage(name), sid)
			}
			if !slices.Equal(actions, tc.want) {
				t.Fatalf("actions=%v want=%v", actions, tc.want)
			}
		})
	}
}

func TestAckBatchDoesNotMoveAcrossTaskBoundary(t *testing.T) {
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
	n := NewAckNotifier(nil, nil, nil, q)
	visible := map[string]bool{}
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
		if recall {
			delete(visible, msg.MessageID)
		} else {
			visible[msg.MessageID] = true
		}
		return nil
	}
	for i, name := range []string{"a", "b"} {
		id := sessionUUID(byte(92 + i))
		q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true, ChannelContextRevision: pgtype.Int8{Int64: 1, Valid: true}}
		n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
	a := q.rows[sessionUUID(92)]
	a.TaskID = sessionUUID(94)
	q.rows[a.ID] = a
	n.OnIngested(context.Background(), inst, groupReactionMessage("b"), sid)
	if len(visible) != 2 {
		t.Fatalf("new batch removed old receipt: %v", visible)
	}
	n.onInputsSettled(context.Background(), sid, []db.ChatMessage{a})
	if len(visible) != 1 || !visible["b"] {
		t.Fatalf("old terminal cleared new batch: %v", visible)
	}
}

func TestAckTerminalBeforeIngestDoesNotRecreateReceipt(t *testing.T) {
	inst, sid, id := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91), sessionUUID(92)
	row := db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
	q := &ackInputQueries{rows: map[pgtype.UUID]db.ChatMessage{id: row}}
	n := NewAckNotifier(nil, nil, nil, q)
	calls := 0
	n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
		calls++
		return nil
	}
	// Even the local source capture may follow a fast terminal event.
	n.onInputsSettled(context.Background(), sid, []db.ChatMessage{row})
	n.client.rememberReplySource(inst.ID, id, groupReactionMessage("a"))
	n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
	if calls != 0 || len(n.active) != 0 {
		t.Fatalf("late hook recreated receipt: calls=%d active=%v", calls, n.active)
	}
}

func TestAckBatchRetriesWhenInputSealsDuringLookup(t *testing.T) {
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
	n := NewAckNotifier(nil, nil, nil, q)
	visible := map[string]bool{}
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
		if recall {
			delete(visible, msg.MessageID)
		} else {
			visible[msg.MessageID] = true
		}
		return nil
	}
	for i, name := range []string{"a", "b"} {
		id := sessionUUID(byte(92 + i))
		q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
		n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
	q.read = func(id pgtype.UUID) {
		if id == sessionUUID(92) {
			q.read = nil
			for id, row := range q.rows {
				row.TaskID = sessionUUID(94)
				q.rows[id] = row
			}
		}
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("b"), sid)
	if len(visible) != 1 || !visible["b"] {
		t.Fatalf("sealing race left wrong receipts: %v", visible)
	}
}

func TestAckBatchRecallsSupersededInFlightAdd(t *testing.T) {
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
	n := NewAckNotifier(nil, nil, nil, q)
	for i, name := range []string{"a", "b"} {
		id := sessionUUID(byte(92 + i))
		q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
		n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
	}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	visible := map[string]bool{}
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
		if msg.MessageID == "a" && !recall {
			close(started)
			<-release
		}
		mu.Lock()
		defer mu.Unlock()
		if recall {
			delete(visible, msg.MessageID)
		} else {
			visible[msg.MessageID] = true
		}
		return nil
	}
	go func() { defer close(done); n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("add did not begin")
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("b"), sid)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late add did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(visible) != 1 || !visible["b"] {
		t.Fatalf("old add reappeared: %v", visible)
	}
}

func TestAckBatchQueryFailureSkipsReceipt(t *testing.T) {
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{err: errors.New("unavailable")}
	n := NewAckNotifier(nil, nil, nil, q)
	n.client.rememberReplySource(inst.ID, sessionUUID(92), groupReactionMessage("a"))
	n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
		t.Fatal("query failure guessed a batch")
		return nil
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
	if len(n.active) != 0 {
		t.Fatal("failed query retained receipt")
	}
}

func TestAckBatchRetriesFailedRecallOnNextMessage(t *testing.T) {
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
	n := NewAckNotifier(nil, nil, nil, q)
	visible := map[string]bool{}
	failed := false
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
		if recall && msg.MessageID == "a" && !failed {
			failed = true
			return errors.New("temporary recall failure")
		}
		if recall {
			delete(visible, msg.MessageID)
		} else {
			visible[msg.MessageID] = true
		}
		return nil
	}
	for i, name := range []string{"a", "b", "c"} {
		id := sessionUUID(byte(92 + i))
		q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
		n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
		n.OnIngested(context.Background(), inst, groupReactionMessage(name), sid)
		if name == "b" && len(visible) != 2 {
			t.Fatalf("fixture did not leave failed recall visible: %v", visible)
		}
	}
	if len(visible) != 1 || !visible["c"] {
		t.Fatalf("failed recall was not retried: %v", visible)
	}
}

func TestAckTerminalDuringBatchLookupDoesNotRecreateReceipt(t *testing.T) {
	inst, sid, id := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91), sessionUUID(92)
	row := db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
	q := &ackInputQueries{rows: map[pgtype.UUID]db.ChatMessage{id: row}}
	n := NewAckNotifier(nil, nil, nil, q)
	n.client.rememberReplySource(inst.ID, id, groupReactionMessage("a"))
	q.read = func(pgtype.UUID) {
		q.read = nil
		n.onInputsSettled(context.Background(), sid, []db.ChatMessage{row})
	}
	n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
		t.Fatal("terminal event during lookup recreated receipt")
		return nil
	}
	n.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
}

func TestAckBatchRejectsUnattributableInput(t *testing.T) {
	for _, kind := range []string{"cache miss", "other installation", "other conversation", "other session", "web input", "assistant input"} {
		t.Run(kind, func(t *testing.T) {
			inst, sid, id := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91), sessionUUID(92)
			row := db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
			q := &ackInputQueries{rows: map[pgtype.UUID]db.ChatMessage{}}
			n := NewAckNotifier(nil, nil, nil, q)
			msg := groupReactionMessage("a")
			if kind != "cache miss" {
				n.client.rememberReplySource(inst.ID, id, msg)
			}
			switch kind {
			case "other installation":
				inst.ID = sessionUUID(93)
			case "other conversation":
				msg.Source.ChatID = "another-chat"
			case "other session":
				row.ChatSessionID = sessionUUID(94)
			case "web input":
				row.ChannelIngested = false
			case "assistant input":
				row.Role = "assistant"
			}
			q.rows[id] = row
			n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
				t.Fatal("unattributable input received reaction")
				return nil
			}
			n.OnIngested(context.Background(), inst, msg, sid)
		})
	}
}

func TestAckTerminalFenceIsBounded(t *testing.T) {
	n := NewAckNotifier(nil, nil, nil, nil)
	sid := sessionUUID(91)
	var first, last pgtype.UUID
	for i := 0; i <= maxReplySources; i++ {
		last = dbid.NewV7()
		if i == 0 {
			first = last
		}
		n.onInputsSettled(context.Background(), sid, []db.ChatMessage{{ID: last, ChatSessionID: sid, Role: "user", ChannelIngested: true}})
	}
	if len(n.settledInputs) != maxReplySources || n.settledInputs[first] || !n.settledInputs[last] {
		t.Fatalf("terminal fence did not evict oldest entry: count=%d first=%v last=%v", len(n.settledInputs), n.settledInputs[first], n.settledInputs[last])
	}
}

func TestAckConcurrentBatchKeepsLastInput(t *testing.T) {
	const count = 24
	inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
	q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
	n := NewAckNotifier(nil, nil, nil, q)
	var mu sync.Mutex
	visible := make(map[string]bool)
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, _ string, recall bool) error {
		mu.Lock()
		defer mu.Unlock()
		if recall {
			delete(visible, msg.MessageID)
		} else {
			visible[msg.MessageID] = true
		}
		return nil
	}
	var messages []channel.InboundMessage
	for i := 0; i < count; i++ {
		id := sessionUUID(byte(100 + i))
		msg := groupReactionMessage(string(rune('a' + i)))
		messages = append(messages, msg)
		q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, Role: "user", ChannelIngested: true}
		n.client.rememberReplySource(inst.ID, id, msg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Make every hook begin from the same empty snapshot, then compete to
	// publish. The last persisted input must win regardless of hook order.
	ready := make(chan struct{})
	seen := make(map[pgtype.UUID]bool)
	q.read = func(id pgtype.UUID) {
		mu.Lock()
		if !seen[id] {
			seen[id] = true
			if len(seen) == count {
				close(ready)
			}
		}
		mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
		}
	}
	var hooks sync.WaitGroup
	for _, msg := range messages {
		hooks.Add(1)
		go func() {
			defer hooks.Done()
			n.OnIngested(ctx, inst, msg, sid)
		}()
	}
	hooks.Wait()
	if ctx.Err() != nil {
		t.Fatal("concurrent hooks did not finish")
	}
	if len(visible) != 1 || !visible[messages[count-1].MessageID] {
		t.Fatalf("concurrent batch did not converge to its last input: %v", visible)
	}
}

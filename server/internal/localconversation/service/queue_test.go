package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func queued(id, content string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"requestId":%q,"content":%q,"display":{"id":%q,"createdAtUnixMs":1,"content":[{"type":"text","text":%q}]}}`, id, content, id, content))
}

const turnDone = `{"type":"result","subtype":"success","is_error":false,"result":""}`

func (f *serviceFixture) sends(t *testing.T) []string {
	t.Helper()
	var texts []string
	for _, a := range f.allAdapters() {
		a.mu.Lock()
		for i, typ := range a.requests {
			if typ == "send" {
				texts = append(texts, a.fields[i]["content"].([]map[string]any)[0]["text"].(string))
			}
		}
		a.mu.Unlock()
	}
	return texts
}

func (f *serviceFixture) allAdapters() []*fakeAdapter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeAdapter(nil), f.adapters...)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestQueueRunsAfterTheCurrentTurn(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, TurnOrigin{})
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("enqueue = %+v, %v", view, err)
	}
	// The same item again is a no-op; another item under its id is not.
	if again, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, TurnOrigin{}); err != nil || len(again.Items) != 1 {
		t.Fatalf("repeat = %+v, %v", again, err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "other"), false, TurnOrigin{}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("conflict = %v", err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q2", "third"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	conv, _ := f.svc.Store().Conversation(ctx, "conv-1")
	if conv.QueueCount != 2 {
		t.Fatalf("queue count = %d", conv.QueueCount)
	}

	f.adapters[0].emit(t, turnDone)
	eventually(t, "the first queued message", func() bool { return fmt.Sprint(f.sends(t)) == "[first second]" })
	f.adapters[0].emit(t, turnDone)
	eventually(t, "the second queued message", func() bool { return fmt.Sprint(f.sends(t)) == "[first second third]" })
	eventually(t, "an empty queue", func() bool {
		view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
		conv, _ := f.svc.Store().Conversation(ctx, "conv-1")
		return len(view.Items) == 0 && conv.QueueCount == 0
	})
	// A message that was already sent is not queued again.
	if view, _ := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, TurnOrigin{}); len(view.Items) != 0 {
		t.Fatalf("re-enqueued a sent message: %+v", view)
	}
}

func TestQueueActions(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued(id, id), id == "a", TurnOrigin{}); err != nil {
			t.Fatal(err)
		}
	}
	view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
	if view.Paused {
		t.Fatal("a later unpaused enqueue should resume the queue")
	}
	if view, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "b"); err != nil || len(view.Items) != 2 {
		t.Fatalf("remove = %+v, %v", view, err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "missing"); err != nil {
		t.Fatalf("removing an unknown item = %v", err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "jump", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown action = %v", err)
	}

	// send_now interrupts the running turn and runs the item next.
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "send_now", "c"); err != nil {
		t.Fatal(err)
	}
	if got := f.adapters[0].sent(); !strings.Contains(fmt.Sprint(got), "interrupt") {
		t.Fatalf("adapter requests = %v", got)
	}
	f.adapters[0].emit(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["interrupted"]}`)
	eventually(t, "the moved item", func() bool { return fmt.Sprint(f.sends(t)) == "[first c]" })
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "c"); !errors.Is(err, ErrQueueItemStarted) {
		t.Fatalf("removing a started item = %v", err)
	}

	// stop pauses: nothing runs after the turn ends until resume.
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "stop", ""); err != nil {
		t.Fatal(err)
	}
	f.adapters[0].emit(t, turnDone)
	time.Sleep(50 * time.Millisecond)
	if got := f.sends(t); len(got) != 2 {
		t.Fatalf("a stopped queue sent %v", got)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the resumed item", func() bool { return fmt.Sprint(f.sends(t)) == "[first c a]" })
}

func TestQueueRejectsInvalidItems(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	for _, raw := range []string{
		`{"requestId":"a","content":"x","display":{"id":"other"}}`,
		`{"requestId":"a","content":"","display":{"id":"a"}}`,
		`{"requestId":"a","content":"x","display":{"id":"a"},"agents":[{"id":"g","name":"G"}]}`,
		`{"requestId":"a","content":"x","display":{"id":"a"},"permissionMode":"yolo"}`,
	} {
		if _, err := f.svc.Enqueue(context.Background(), f.scopeID, "conv-1", json.RawMessage(raw), false, TurnOrigin{}); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestQueueRejectsAnItemPastCapacity(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	item := func(id string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"requestId":%q,"content":"x","display":{"id":%q}}`, id, id))
	}
	for i := 0; i < maxQueueItems; i++ {
		if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", item(fmt.Sprintf("q%d", i)), true, TurnOrigin{}); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", item("over"), true, TurnOrigin{}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("over capacity = %v", err)
	}
	// A retry of a queued item is not a new item.
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", item("q0"), true, TurnOrigin{}); err != nil {
		t.Fatalf("retry of a queued item = %v", err)
	}
	view, err := f.svc.Queue(ctx, f.scopeID, "conv-1")
	if err != nil || len(view.Items) != maxQueueItems {
		t.Fatalf("queue = %d items, %v", len(view.Items), err)
	}
}

func TestQueueWaitsForTheUserAfterRestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "c.sqlite")
	f := newServiceFixture(t, path)
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "after restart"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Close(ctx); err != nil {
		t.Fatal(err)
	}

	again := newServiceFixture(t, path)
	view, err := again.svc.Queue(ctx, again.scopeID, "conv-1")
	if err != nil || !view.Paused || view.Error != queuePausedByRestart || len(view.Items) != 1 {
		t.Fatalf("queue after restart = %+v, %v; want it paused with its message", view, err)
	}
	time.Sleep(50 * time.Millisecond)
	if sent := again.sends(t); len(sent) != 0 {
		t.Fatalf("sent %v before the user resumed the queue", sent)
	}
	if _, err := again.svc.UpdateQueue(ctx, again.scopeID, "conv-1", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the queued message after resume", func() bool { return fmt.Sprint(again.sends(t)) == "[after restart]" })
}

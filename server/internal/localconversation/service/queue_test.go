package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
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

func TestQueueWaitsBeforeApplyingNextTurnSettings(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	before, err := f.svc.Store().Conversation(ctx, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"requestId":"q1","content":"second","permissionMode":"auto","display":{"id":"q1"}}`)
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", raw, false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	// Join a dispatch attempt through the same queue lock without waiting for the active turn.
	f.svc.dispatchQueue("conv-1")
	after, err := f.svc.Store().Conversation(ctx, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.PermissionMode != before.PermissionMode {
		t.Fatalf("queued settings changed the running turn: %q -> %q", before.PermissionMode, after.PermissionMode)
	}
	if got := f.sends(t); fmt.Sprint(got) != "[first]" {
		t.Fatalf("sent while the first turn was running: %v", got)
	}
	f.adapters[0].emit(t, turnDone)
	eventually(t, "the queued message", func() bool { return fmt.Sprint(f.sends(t)) == "[first second]" })
	after, err = f.svc.Store().Conversation(ctx, "conv-1")
	if err != nil || after.PermissionMode != "auto" {
		t.Fatalf("queued turn settings = %q, %v", after.PermissionMode, err)
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

func TestMemberReferenceSurvivesQueueRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.sqlite")
	f := newServiceFixture(t, path)
	f.create(t, "conv-1", "claude")
	text := "Assign to [@Alex](mention://workspace-member/user-b?workspaceId=w&userId=user-b)"
	var raw map[string]any
	if err := json.Unmarshal(queued("member-queued", text), &raw); err != nil {
		t.Fatal(err)
	}
	refs := []Reference{{Kind: "member", ID: "user-b", WorkspaceID: "w"}}
	raw["references"] = refs
	item, _ := json.Marshal(raw)
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", item, true, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Close(ctx); err != nil {
		t.Fatal(err)
	}
	again := newServiceFixture(t, path)
	view, err := again.svc.Queue(ctx, again.scopeID, "conv-1")
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("queue = %+v, %v", view, err)
	}
	parsed, err := parseQueueItem(view.Items[0])
	if err != nil || len(parsed.References) != 1 || parsed.References[0] != refs[0] {
		t.Fatalf("restored item = %+v, %v", parsed, err)
	}
	if _, err := again.svc.UpdateQueue(ctx, again.scopeID, "conv-1", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the member prompt after restart", func() bool { return len(again.sends(t)) == 1 })
	if got := again.sends(t)[0]; got != text {
		t.Fatalf("provider prompt = %s", got)
	}
	again.svc.mu.Lock()
	runner := again.svc.runners["conv-1"].(*ClaudeRunner)
	again.svc.mu.Unlock()
	if got := fmt.Sprint(runner.currentContext()["references"]); got != fmt.Sprint(refs) {
		t.Fatalf("current references = %s", got)
	}
}

func TestQueueResumeRetriesAMessageTheProviderNeverReceived(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	var refused atomic.Bool
	refused.Store(true)
	f.reply = func(_ *fakeAdapter, typ string) (json.RawMessage, error) {
		if typ == "send" && refused.Load() {
			return nil, &claude.AdapterError{Code: "refused", Message: "refused", Dispatch: "not_dispatched"}
		}
		return nil, nil
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "hello"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the queue to pause on the failure", func() bool {
		view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
		return view.Paused && view.Error != "" && len(view.Items) == 1
	})

	refused.Store(false)
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the message to reach the provider", func() bool {
		for _, a := range f.allAdapters() {
			a.mu.Lock()
			for i, typ := range a.requests {
				if typ == "send" && a.fields[i]["request_id"] != "q1" {
					a.mu.Unlock()
					return true
				}
			}
			a.mu.Unlock()
		}
		return false
	})
	eventually(t, "the queue to empty", func() bool {
		view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
		return len(view.Items) == 0 && !view.Paused
	})
	// The failed attempt stays in the conversation, and the retry is a turn of its own.
	turns, err := f.svc.Store().ConversationTurns(ctx, "conv-1")
	if err != nil || len(turns) != 2 || turns[0].Status != "failed" || turns[1].Status != "running" {
		t.Fatalf("turns = %+v, %v", turns, err)
	}
	// Enqueuing the original id again is still a no-op: the message was handled.
	if again, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "hello"), false, TurnOrigin{}); err != nil || len(again.Items) != 0 {
		t.Fatalf("re-enqueued a handled message: %+v, %v", again, err)
	}
}

// queueUpdates returns the payloads of the conversation's queue.updated events.
func queueUpdates(t *testing.T, f *serviceFixture, conversationID string) []map[string]any {
	t.Helper()
	rows, err := f.svc.Store().EventsAfter(context.Background(), conversationID, 0, 1<<40, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var updates []map[string]any
	for _, row := range rows {
		if row.Type != "queue.updated" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		updates = append(updates, payload)
	}
	return updates
}

func TestQueueUpdatesSayWhatChangedAndWhy(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued(id, id), false, TurnOrigin{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "a"); err != nil {
		t.Fatal(err)
	}
	f.adapters[0].emit(t, turnDone)
	eventually(t, "the remaining item", func() bool { return fmt.Sprint(f.sends(t)) == "[first b]" })
	eventually(t, "its dispatch recorded", func() bool { return len(queueUpdates(t, f, "conv-1")) == 4 })

	var got []string
	for _, update := range queueUpdates(t, f, "conv-1") {
		got = append(got, fmt.Sprint(update["reason"], update["request_ids"]))
	}
	want := "[enqueued[a] enqueued[a b] removed[b] dispatched[]]"
	if fmt.Sprint(got) != want {
		t.Fatalf("updates = %v, want %s", got, want)
	}
}

func TestQueueDoesNotRestoreARemovedMessage(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "q1"); err != nil {
		t.Fatal(err)
	}
	// A client retrying an enqueue whose answer it lost must not bring it back.
	view, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, TurnOrigin{})
	if err != nil || len(view.Items) != 0 {
		t.Fatalf("retried enqueue = %+v, %v", view, err)
	}
}

func TestQueueRetryIgnoresHowTheMessageIsDisplayed(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	shown := func(at int) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"requestId":"q1","content":"second","display":{"id":"q1","clientSubmitId":"q1","createdAtUnixMs":%d}}`, at))
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", shown(1), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	// A reload rebuilds the display with a new time; the message is the same.
	view, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", shown(2), false, TurnOrigin{})
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("retried enqueue = %+v, %v", view, err)
	}
}

func TestSendNowOnAPausedQueueLeavesTheRestPaused(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued(id, id), true, TurnOrigin{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "send_now", "b"); err != nil {
		t.Fatal(err)
	}
	f.adapters[0].emit(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["interrupted"]}`)
	eventually(t, "the chosen item", func() bool { return fmt.Sprint(f.sends(t)) == "[first b]" })
	eventually(t, "the chosen item leaving the queue", func() bool {
		view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
		return len(view.Items) == 1
	})
	view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1")
	if !view.Paused {
		t.Fatal("resuming one message resumed the whole queue")
	}
	f.adapters[0].emit(t, turnDone)
	time.Sleep(50 * time.Millisecond)
	if got := f.sends(t); fmt.Sprint(got) != "[first b]" {
		t.Fatalf("a paused queue sent %v", got)
	}
}

func TestPromptStatuses(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "sent", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	// Hold the queue so a dispatch attempt cannot change the status being asserted.
	for _, id := range []string{"waiting", "removed"} {
		if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued(id, id), true, TurnOrigin{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv-1", "remove", "removed"); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.PromptStatuses(ctx, f.scopeID, "conv-1", []string{"sent", "waiting", "removed", "never"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"sent": "delivered", "waiting": "queued", "removed": "removed", "never": "none"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	if _, err := f.svc.PromptStatuses(ctx, f.scopeID, "conv-1", make([]string, maxQueueItems+1)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("too many ids = %v", err)
	}
}

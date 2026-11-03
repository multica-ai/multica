package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestSubagentTranscriptPersistsUnderOriginalTurnWhileRootContinues(t *testing.T) {
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	t.Cleanup(func() { _ = f.runner.Close(context.Background()) })
	send(t, f, "t1")
	a := f.adapter(0)
	a.emit(t,
		`{"type":"assistant","uuid":"root-message","message":{"id":"root","content":[{"type":"tool_use","id":"agent-a","name":"Agent","input":{"prompt":"Review"}}]}}`,
		`{"type":"system","subtype":"task_started","task_id":"task-a","tool_use_id":"agent-a","task_type":"local_agent","description":"Review"}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"agent-a","content":"Running in background"}]}}`,
		`{"type":"result","result":"Review running"}`,
	)
	f.turnEvents(t, "t1")
	// Child output arriving while no foreground turn runs must not start one.
	a.emit(t, `{"type":"assistant","parent_tool_use_id":"agent-a","uuid":"child-message","message":{"id":"child","content":[{"type":"text","text":"Reviewing the changes"}]}}`)
	if f.runner.turn != nil {
		t.Fatal("child output started a root turn")
	}
	if _, err := f.batcher.AppendWait(context.Background(), store.Batch{ConversationID: "conv-1"}); err != nil {
		t.Fatal(err)
	}
	live, err := f.store.Snapshot(context.Background(), "conv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	backgroundVisible := false
	for _, e := range live.Turns[0].Events {
		backgroundVisible = backgroundVisible || e.ItemID.String == "task:task-a"
	}
	if !backgroundVisible {
		t.Fatal("completed root turn lost its live background card on reload")
	}
	send(t, f, "t2")
	a.emit(t,
		`{"type":"assistant","parent_tool_use_id":"agent-a","uuid":"child-tool","message":{"id":"child-tools","content":[{"type":"tool_use","id":"call","name":"Read","input":{"file_path":"README.md"}}]}}`,
		`{"type":"user","parent_tool_use_id":"agent-a","message":{"content":[{"type":"tool_result","tool_use_id":"call","content":"missing file","is_error":true}]}}`,
		`{"type":"result","parent_tool_use_id":"agent-a","is_error":true,"result":"child failed"}`,
	)
	if f.runner.turn == nil || f.runner.turn.id != "t2" {
		t.Fatal("child result ended current root turn")
	}
	a.emit(t,
		`{"type":"system","subtype":"task_notification","task_id":"task-a","status":"failed","summary":"Review failed"}`,
		`{"type":"assistant","uuid":"root-second","message":{"id":"second","content":[{"type":"text","text":"Root reply"}]}}`,
		`{"type":"result","result":"Root reply"}`,
	)
	f.turnEvents(t, "t2")
	f.batcher.Close()
	snapshot, err := f.store.Snapshot(context.Background(), "conv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Turns) != 2 {
		t.Fatalf("unexpected root turns: %d", len(snapshot.Turns))
	}
	children := 0
	for _, turn := range snapshot.Turns {
		for _, e := range turn.Events {
			var p map[string]any
			_ = json.Unmarshal([]byte(e.Payload), &p)
			if p["parent_tool_use_id"] == "agent-a" {
				children++
				if turn.Turn.ID != "t1" {
					t.Fatalf("child moved to %s", turn.Turn.ID)
				}
			}
		}
		for _, m := range turn.Messages {
			if strings.Contains(m.Content, "Reviewing the changes") {
				t.Fatal("child text leaked into root messages")
			}
		}
		if turn.Turn.ID == "t2" && turn.Turn.ProviderTurnID != "root-second" {
			t.Fatalf("child corrupted fork boundary: %q", turn.Turn.ProviderTurnID)
		}
	}
	if children != 2 {
		t.Fatalf("persisted child items = %d, want 2", children)
	}
}

func TestRestartKeepsPartialChildMessagesAfterTheRootTurnCompleted(t *testing.T) {
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	t.Cleanup(func() { _ = f.runner.Close(context.Background()) })
	send(t, f, "t1")
	a := f.adapter(0)
	a.emit(t,
		`{"type":"system","subtype":"task_started","task_id":"a","tool_use_id":"parent","task_type":"local_agent","description":"Review"}`,
		`{"type":"result","result":"Running in background"}`,
		`{"type":"stream_event","parent_tool_use_id":"parent","event":{"type":"message_start","message":{"id":"child"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"parent","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"parent","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial review"}}}`,
	)
	f.batcher.Close()
	w := f.store.NewWriter(store.WriterOptions{})
	defer w.Close()
	count, err := CloseOrphanedBackgroundTasks(context.Background(), f.store, w, time.Now())
	if err != nil || count != 1 {
		t.Fatalf("restart closed %d tasks: %v", count, err)
	}
	snapshot, err := f.store.Snapshot(context.Background(), "conv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range snapshot.Turns[0].Events {
		var p map[string]any
		_ = json.Unmarshal([]byte(e.Payload), &p)
		if p["kind"] == "subagent" {
			found = p["text"] == "Partial review" && p["status"] == "cancelled" && e.Final == 1
		}
	}
	if !found {
		t.Fatal("restart lost partial child transcript")
	}
}

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSubmissionClaimAndFormalHandoffAreAtomic(t *testing.T) {
	s, _ := openTestStore(t)
	scope := createConversation(t, s, "conv")
	w := s.NewWriter(WriterOptions{})
	defer w.Close()
	ctx := context.Background()
	raw := json.RawMessage(`{"requestId":"r","content":"hello","display":{"id":"r"}}`)
	claim := SubmissionAttempt{AttemptID: "attempt", RequestID: "native", Settings: json.RawMessage(`{}`)}
	if _, err := w.Submit(ctx, Batch{ConversationID: "conv", Queue: &QueueChange{Revision: 1, Append: &QueueItem{RequestID: "r", Item: raw}}, Submissions: []SubmissionChange{{RequestID: "r", Claim: &claim}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Submit(ctx, Batch{ConversationID: "conv", Submissions: []SubmissionChange{{RequestID: "r", Claim: &SubmissionAttempt{AttemptID: "competing", RequestID: "other"}}}}); !errors.Is(err, ErrExecutionBusy) {
		t.Fatalf("competing claim=%v", err)
	}
	// An invalid event rejects the entire handoff, including its receipt and queue removal.
	failed := Batch{ConversationID: "conv", StartTurn: &NewTurn{ID: "turn", RequestID: "native", StartedAt: testTime}, Messages: []NewMessage{{ID: "msg-native", TurnID: "turn", Role: "user", Content: "hello", CreatedAt: testTime}}, Receipt: &CommandReceipt{ScopeID: scope, RequestID: "native", Fingerprint: "same", Result: `{}`, CreatedAt: testTime}, Events: []NewEvent{event("missing-turn", "", "user.message", true, `{}`)}}
	// Invalid JSON fails at the SQL/event boundary even without foreign keys.
	failed.Events[0].Payload = json.RawMessage(`{`)
	if _, err := w.Submit(ctx, failed); err == nil {
		t.Fatal("invalid handoff committed")
	}
	_, queue, values, err := s.SubmissionState(ctx, "conv", []string{"r"})
	if err != nil || len(queue.Items) != 1 || values[0].State != "starting" {
		t.Fatalf("partial handoff: %+v %+v %v", queue, values, err)
	}
	if _, err := s.Receipt(ctx, scope, "native"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("receipt escaped rollback: %v", err)
	}
	failed.Events = []NewEvent{event("turn", "", "user.message", true, `{"message_id":"msg-native","request_id":"native","content":"hello"}`), event("turn", "", "turn.started", false, `{"request_id":"native"}`)}
	if _, err := w.Submit(ctx, failed); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, "conv", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Queue.Items) != 0 || len(snap.Submissions) != 1 || snap.Submissions[0].State != "attached" || snap.Submissions[0].MessageID != "msg-native" {
		t.Fatalf("handoff=%+v %+v", snap.Queue, snap.Submissions)
	}
	events, err := s.TurnEvents(ctx, "conv", "turn")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Type == "user.message" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
				t.Fatal(err)
			}
			found = payload["submission_id"] == "r"
		}
	}
	if !found {
		t.Fatal("formal message lost the logical submission identity")
	}
	if _, err := s.Receipt(ctx, scope, "native"); err != nil {
		t.Fatal(err)
	}
}

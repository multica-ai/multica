package store

import (
	"context"
	"testing"
	"time"
)

func TestSummarySendTimeTracksUserTurnsAcrossReadsAndRestart(t *testing.T) {
	t.Parallel()
	s, path := openTestStore(t)
	scopeID := createConversation(t, s, "recency")
	ctx := context.Background()
	w := s.NewWriter(WriterOptions{})
	defer w.Close()
	assertTime := func(want string) {
		t.Helper()
		summary, err := s.Summary(ctx, "recency")
		if err != nil || summary.LastTurnSentAt != want {
			t.Fatalf("summary send time = %q, %v; want %q", summary.LastTurnSentAt, err, want)
		}
		rows, _, err := s.SummariesAfter(ctx, scopeID, 0, 100)
		if err != nil || len(rows) != 1 || rows[0].LastTurnSentAt != want {
			t.Fatalf("list = %+v, %v", rows, err)
		}
		snap, err := s.Snapshot(ctx, "recency", 1)
		if err != nil || snap.Conversation.LastTurnSentAt != want {
			t.Fatalf("bounded snapshot = %+v, %v", snap.Conversation, err)
		}
	}
	submit := func(b Batch) {
		t.Helper()
		b.ConversationID = "recency"
		if b.Summary == nil {
			b.Summary = &SummaryPatch{UpdatedAt: testTime.Add(time.Hour)}
		}
		commit, err := w.Submit(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		read, err := s.Summary(ctx, "recency")
		if err != nil || commit.Summary == nil || commit.Summary.LastTurnSentAt != read.LastTurnSentAt {
			t.Fatalf("live summary disagrees with read: %+v, %+v, %v", commit.Summary, read, err)
		}
	}
	assertTime("")
	submit(Batch{StartTurn: &NewTurn{ID: "visible", StartedAt: testTime},
		Messages: []NewMessage{{ID: "user", TurnID: "visible", Role: "user", CreatedAt: testTime}}})
	want := formatTime(testTime)
	assertTime(want)
	submit(Batch{Messages: []NewMessage{{ID: "assistant", TurnID: "visible", Role: "assistant", CreatedAt: testTime.Add(time.Minute)}}})
	assertTime(want)
	submit(Batch{Messages: []NewMessage{{ID: "steer", TurnID: "visible", Role: "user", Steered: true, CreatedAt: testTime.Add(2 * time.Minute)}}})
	assertTime(want)
	submit(Batch{CompleteTurn: &TurnCompletion{TurnID: "visible", Status: "completed", CompletedAt: testTime.Add(3 * time.Minute)}})
	assertTime(want)
	// The newest visible user turn may be outside the bounded history page.
	submit(Batch{StartTurn: &NewTurn{ID: "hidden", StartedAt: testTime.Add(4 * time.Minute)}})
	assertTime(want)
	count := int64(1)
	submit(Batch{Summary: &SummaryPatch{QueueCount: &count, UpdatedAt: testTime.Add(5 * time.Minute)}})
	assertTime(want)
	next := testTime.Add(6 * time.Minute)
	submit(Batch{StartTurn: &NewTurn{ID: "next", StartedAt: next},
		Messages: []NewMessage{{ID: "next-user", TurnID: "next", Role: "user", CreatedAt: next}}})
	want = formatTime(next)
	assertTime(want)
	w.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	row, err := reopened.Summary(ctx, "recency")
	if err != nil || row.LastTurnSentAt != want {
		t.Fatalf("reopened = %+v, %v", row, err)
	}
}

func TestSummarySendTimePreservesImportedAndForkedHistory(t *testing.T) {
	t.Parallel()
	s, _ := openTestStore(t)
	scopeID := createConversation(t, s, "empty")
	ctx := context.Background()
	var history []Batch
	for i, id := range []string{"first", "last"} {
		at := testTime.Add(time.Duration(i) * time.Minute)
		history = append(history, Batch{StartTurn: &NewTurn{ID: id, StartedAt: at},
			Messages:     []NewMessage{{ID: id + "-user", TurnID: id, Role: "user", CreatedAt: at}},
			CompleteTurn: &TurnCompletion{TurnID: id, Status: "completed", CompletedAt: at.Add(time.Hour)},
			Summary:      &SummaryPatch{UpdatedAt: at.Add(time.Hour)}})
	}
	if _, err := s.ImportConversation(ctx, NewConversation{ID: "imported", ScopeID: scopeID, Provider: "codex", CreatedAt: testTime.Add(24 * time.Hour)}, history); err != nil {
		t.Fatal(err)
	}
	for _, fork := range []ForkCopy{
		{SourceID: "imported", ID: "full", CreatedAt: testTime.Add(48 * time.Hour)},
		{SourceID: "imported", ID: "boundary", ThroughTurnID: "first", CreatedAt: testTime.Add(48 * time.Hour)},
	} {
		if _, err := s.ForkConversation(ctx, fork); err != nil {
			t.Fatal(err)
		}
	}
	for id, at := range map[string]time.Time{"imported": testTime.Add(time.Minute), "full": testTime.Add(time.Minute), "boundary": testTime} {
		row, err := s.Summary(ctx, id)
		if err != nil || row.LastTurnSentAt != formatTime(at) {
			t.Fatalf("%s = %+v, %v", id, row, err)
		}
	}
}

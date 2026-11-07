package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionMigrationBaselinesExistingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := schemaVersions()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range versions[:7] {
		ddl, err := schemaFS.ReadFile(version.file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(ddl)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO scope (id, api_origin, user_id, workspace_id) VALUES (1, 'api', 'user', 'workspace');
INSERT INTO conversation (id, scope_id, provider, turn_count, created_at, updated_at) VALUES ('c', 1, 'claude', 2, 'now', 'now');
INSERT INTO turn (id, conversation_id, turn_index, status, started_at) VALUES ('success', 'c', 0, 'completed', 'now'), ('failure', 'c', 1, 'failed', 'now');
PRAGMA user_version = 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := s.Conversation(context.Background(), "c")
	if err != nil || c.CompletedTurnID != "success" || c.ReadTurnIndex != 0 || c.CompletedTurnIndex != 0 {
		t.Fatalf("baseline: %+v, %v", c, err)
	}
	if _, err := os.Stat(path + ".v7.bak"); err != nil {
		t.Fatalf("upgrade backup: %v", err)
	}
}

func TestCompletionReadKeepsNewerSuccessAndPersists(t *testing.T) {
	s, path := openTestStore(t)
	createConversation(t, s, "c")
	ctx := context.Background()
	w := s.NewWriter(WriterOptions{})
	submit := func(b Batch) {
		t.Helper()
		b.ConversationID = "c"
		if _, err := w.Submit(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b"} {
		submit(Batch{StartTurn: &NewTurn{ID: id, StartedAt: testTime}})
		submit(Batch{CompleteTurn: &TurnCompletion{TurnID: id, Status: "completed", CompletedAt: testTime}})
	}
	before, _ := s.Summary(ctx, "c")
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "a", TurnIndex: 0, CompletionSeq: 1}})
	row, _ := s.Summary(ctx, "c")
	if row.CompletedTurnID != "b" || row.ReadTurnIndex != 0 || row.CompletedTurnIndex != 1 || row.UpdatedAt != before.UpdatedAt || row.SummarySeq <= before.SummarySeq {
		t.Fatalf("read A changed B or recency: %+v", row)
	}
	for _, status := range []string{"failed", "cancelled"} {
		submit(Batch{StartTurn: &NewTurn{ID: status, StartedAt: testTime}})
		submit(Batch{CompleteTurn: &TurnCompletion{TurnID: status, Status: status, CompletedAt: testTime}})
	}
	for _, read := range []ReadCompletion{{TurnID: "b", TurnIndex: 99, CompletionSeq: 2}, {TurnID: "failed", TurnIndex: 2, CompletionSeq: 3}, {TurnID: "missing", TurnIndex: 0, CompletionSeq: 1}} {
		if _, err := w.Submit(ctx, Batch{ConversationID: "c", ReadCompletion: &read}); !errors.Is(err, ErrInvalidCompletion) {
			t.Fatalf("accepted invalid read %+v: %v", read, err)
		}
	}
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "b", TurnIndex: 1, CompletionSeq: 2}})
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "a", TurnIndex: 0, CompletionSeq: 1}})
	w.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	row, err = reopened.Summary(ctx, "c")
	if err != nil || row.CompletedTurnID != "b" || row.ReadTurnIndex != 1 {
		t.Fatalf("reopened: %+v, %v", row, err)
	}
}

func TestCompletionOrderIsIndependentOfTurnOrder(t *testing.T) {
	s, _ := openTestStore(t)
	createConversation(t, s, "c")
	w := s.NewWriter(WriterOptions{})
	defer w.Close()
	ctx := context.Background()
	submit := func(b Batch) {
		t.Helper()
		b.ConversationID = "c"
		if _, err := w.Submit(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	submit(Batch{StartTurn: &NewTurn{ID: "a", StartedAt: testTime}, MoreTurns: []NewTurn{{ID: "b", StartedAt: testTime}}})
	submit(Batch{CompleteTurn: &TurnCompletion{TurnID: "b", Status: "completed", CompletedAt: testTime}})
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "b", TurnIndex: 1, CompletionSeq: 1}})
	submit(Batch{CompleteTurn: &TurnCompletion{TurnID: "a", Status: "completed", CompletedAt: testTime}})
	row, _ := s.Summary(ctx, "c")
	if row.CompletedTurnID != "a" || row.CompletedTurnIndex != 0 || row.CompletionSeq <= row.ReadCompletionSeq {
		t.Fatalf("late lower-index result was lost: %+v", row)
	}
	// A repeated terminal report for B must not turn an old result into a new one.
	submit(Batch{CompleteTurn: &TurnCompletion{TurnID: "b", Status: "completed", CompletedAt: testTime}})
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "b", TurnIndex: 1, CompletionSeq: 1}})
	row, _ = s.Summary(ctx, "c")
	if row.CompletedTurnID != "a" || row.CompletionSeq != 2 || row.ReadCompletionSeq != 1 {
		t.Fatalf("old B replaced A: %+v", row)
	}
	submit(Batch{ReadCompletion: &ReadCompletion{TurnID: "a", TurnIndex: 0, CompletionSeq: 2}})
	row, _ = s.Summary(ctx, "c")
	if row.CompletionSeq != row.ReadCompletionSeq {
		t.Fatalf("A remained unread: %+v", row)
	}
}

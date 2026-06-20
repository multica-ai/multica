package note

// FIR-1621 — per-comment "sent to agent" state. A note comment starts as an
// unsent local draft (sent_to_agent_at IS NULL) and is stamped when explicitly
// sent. These tests exercise the cerebro query layer against a real DB and skip
// cleanly when none is reachable (same pattern as wave3_db_test.go).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestSentMarkAndCount(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Sent", "draft body for sending")

	// Three unsent draft comments.
	var ids []pgtype.UUID
	for _, body := range []string{"first", "second", "third"} {
		c, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
			NoteID:     noteID,
			Kind:       "comment",
			Body:       body,
			AuthorType: "member",
			AuthorID:   w3UserA,
		})
		if err != nil {
			t.Fatalf("create comment %q: %v", body, err)
		}
		if c.SentToAgentAt.Valid {
			t.Fatalf("new comment %q should be unsent", body)
		}
		ids = append(ids, c.ID)
	}

	if n, _ := w3H.Cerebro.CountUnsentNoteComments(ctx, noteID); n != 3 {
		t.Fatalf("unsent count = %d, want 3", n)
	}

	// Send the first two.
	updated, err := w3H.Cerebro.MarkNoteCommentsSent(ctx, cerebrodb.MarkNoteCommentsSentParams{
		NoteID:     noteID,
		CommentIds: []pgtype.UUID{ids[0], ids[1]},
	})
	if err != nil || len(updated) != 2 {
		t.Fatalf("mark sent: rows=%d err=%v; want 2", len(updated), err)
	}
	var firstSentAt pgtype.Timestamptz
	for _, c := range updated {
		if !c.SentToAgentAt.Valid {
			t.Fatalf("comment %s should be marked sent", uuidStr(c.ID))
		}
		if uuidStr(c.ID) == uuidStr(ids[0]) {
			firstSentAt = c.SentToAgentAt
		}
	}

	if n, _ := w3H.Cerebro.CountUnsentNoteComments(ctx, noteID); n != 1 {
		t.Fatalf("unsent count after send = %d, want 1", n)
	}

	// Re-sending a batch that includes an already-sent comment is idempotent:
	// the original send timestamp is preserved (COALESCE), not rewritten.
	reUpdated, err := w3H.Cerebro.MarkNoteCommentsSent(ctx, cerebrodb.MarkNoteCommentsSentParams{
		NoteID:     noteID,
		CommentIds: []pgtype.UUID{ids[0], ids[2]},
	})
	if err != nil {
		t.Fatalf("re-mark sent: %v", err)
	}
	for _, c := range reUpdated {
		if uuidStr(c.ID) == uuidStr(ids[0]) && !c.SentToAgentAt.Time.Equal(firstSentAt.Time) {
			t.Fatalf("already-sent comment timestamp was rewritten: %v != %v", c.SentToAgentAt.Time, firstSentAt.Time)
		}
	}

	if n, _ := w3H.Cerebro.CountUnsentNoteComments(ctx, noteID); n != 0 {
		t.Fatalf("unsent count after sending all = %d, want 0", n)
	}
}

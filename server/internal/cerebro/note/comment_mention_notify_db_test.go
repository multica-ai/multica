package note

// FIR-2589 — a member @mentioned in a COMMENT on a note must get a notification,
// exactly like a member @mentioned in the note body. These tests exercise
// notifyCommentMentions against a real DB and a live event bus, proving:
//   * mentioning a member in a comment publishes EventNoteMentioned with that
//     member id (the note-mention listener turns it into a "mentioned" inbox
//     item), shares the note with them, and bumps a private note to 'shared' so
//     the notification is openable,
//   * mentioning only the comment author publishes nothing and shares nothing
//     (you are never notified about your own mention).
// They skip cleanly when no DB is reachable (same pattern as the other *_db_test).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

// mkComment builds a CerebroNoteComment with a fresh id and the given body, as
// notifyCommentMentions receives it from CreateComment.
func mkComment(noteID pgtype.UUID, body string) cerebrodb.CerebroNoteComment {
	id, _ := uuid.NewV7()
	return cerebrodb.CerebroNoteComment{
		ID:     pgtype.UUID{Bytes: id, Valid: true},
		NoteID: noteID,
		Body:   body,
	}
}

// busHandler builds a handler backed by the shared pool but with a fresh event
// bus, plus a slice that captures every EventNoteMentioned it publishes.
func busHandler() (*Handler, *[]events.Event) {
	bus := events.New()
	captured := &[]events.Event{}
	bus.Subscribe(EventNoteMentioned, func(e events.Event) {
		*captured = append(*captured, e)
	})
	return &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Bus: bus}, captured
}

func TestCommentMentionNotifiesAndShares(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	noteID := makeNote(t, ctx, "Mention target", "body")
	h, captured := busHandler()

	// UserA (the comment author) tags UserB in a comment body.
	body := "look at this [@B](mention://member/" + uuidStr(w3UserB) + ")"
	comment := mkComment(noteID, body)
	h.notifyCommentMentions(ctx, w3WsID, noteID, w3UserA, comment)

	if len(*captured) != 1 {
		t.Fatalf("expected 1 EventNoteMentioned, got %d", len(*captured))
	}
	payload, ok := (*captured)[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload not a map: %T", (*captured)[0].Payload)
	}
	ids, _ := payload["member_ids"].([]string)
	if len(ids) != 1 || ids[0] != uuidStr(w3UserB) {
		t.Fatalf("expected member_ids [%s], got %v", uuidStr(w3UserB), ids)
	}

	// FIR-2589: the payload carries the comment id (inbox deep-link target) and
	// a non-empty excerpt (the inbox message body), so the notification opens the
	// exact comment and reads with context.
	if got := payload["comment_id"]; got != uuidStr(comment.ID) {
		t.Fatalf("expected comment_id %s, got %v", uuidStr(comment.ID), got)
	}
	if got, _ := payload["comment_excerpt"].(string); got != body {
		t.Fatalf("expected comment_excerpt %q, got %q", body, got)
	}

	// The note is now shared with UserB and bumped from private to shared, so
	// the notification the listener creates is openable.
	shares, err := h.Cerebro.ListNoteShares(ctx, noteID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	sharedWithB := false
	for _, s := range shares {
		if uuidStr(s.UserID) == uuidStr(w3UserB) {
			sharedWithB = true
		}
	}
	if !sharedWithB {
		t.Fatal("expected note to be shared with UserB after comment mention")
	}
	noteRow, err := h.Cerebro.GetNote(ctx, noteID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if noteRow.Visibility != "shared" {
		t.Fatalf("expected visibility 'shared', got %q", noteRow.Visibility)
	}
}

func TestCommentSelfMentionNotifiesNoOne(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	noteID := makeNote(t, ctx, "Self mention", "body")
	h, captured := busHandler()

	// The author mentions only themselves — nothing should fire.
	body := "note to self [@A](mention://member/" + uuidStr(w3UserA) + ")"
	h.notifyCommentMentions(ctx, w3WsID, noteID, w3UserA, mkComment(noteID, body))

	if len(*captured) != 0 {
		t.Fatalf("expected no EventNoteMentioned for a self-mention, got %d", len(*captured))
	}
	shares, err := h.Cerebro.ListNoteShares(ctx, noteID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 0 {
		t.Fatalf("expected no shares for a self-mention, got %d", len(shares))
	}
}

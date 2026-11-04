package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDirectiveQuoteJoinsTrimsAndBounds(t *testing.T) {
	batch := []db.ChatMessage{
		{Content: "  first line  "},
		{Content: "   "},
		{Content: "second"},
	}
	if got, want := directiveQuote(batch), "first line\n\nsecond"; got != want {
		t.Errorf("directiveQuote = %q, want %q", got, want)
	}

	long := strings.Repeat("é", maxDirectiveQuoteRunes+10)
	got := directiveQuote([]db.ChatMessage{{Content: long}})
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long quote should be marked as cut, got suffix %q", got[len(got)-3:])
	}
	if n := len([]rune(got)); n != maxDirectiveQuoteRunes+1 {
		t.Errorf("quote rune length = %d, want %d", n, maxDirectiveQuoteRunes+1)
	}
}

func TestDirectiveGrantMetadataShape(t *testing.T) {
	id := pgtype.UUID{Valid: true, Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}
	raw, err := DirectiveGrant{MessageID: id, ChatSessionID: id, OriginatorUserID: id, Quote: "ship it"}.Metadata()
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	var out struct {
		Directive struct {
			Quote         string `json:"quote"`
			MessageID     string `json:"message_id"`
			ChatSessionID string `json:"chat_session_id"`
			Originator    string `json:"originator_user_id"`
		} `json:"directive"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Directive.Quote != "ship it" {
		t.Errorf("quote = %q", out.Directive.Quote)
	}
	if out.Directive.MessageID != "01020304-0506-0708-090a-0b0c0d0e0f10" {
		t.Errorf("message_id = %q", out.Directive.MessageID)
	}
	if out.Directive.Originator == "" || out.Directive.ChatSessionID == "" {
		t.Errorf("session and originator must be recorded: %+v", out.Directive)
	}
}

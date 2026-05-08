package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildGatewayMessagesCapsHistoryAndExcludesPostStartMessages(t *testing.T) {
	started := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	history := []db.ChatMessage{
		{Role: "user", Content: "oldest", CreatedAt: ts(started.Add(-4 * time.Minute))},
		{Role: "assistant", Content: "prior", CreatedAt: ts(started.Add(-3 * time.Minute))},
		{Role: "user", Content: "current", CreatedAt: ts(started.Add(-2 * time.Minute))},
		{Role: "user", Content: "sent after start", CreatedAt: ts(started.Add(time.Second))},
	}

	messages := buildGatewayMessages(db.Agent{
		Name:         "Charlotte",
		Instructions: "Svar kort.",
	}, history, ts(started), 2)

	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want system + 2 history messages: %+v", len(messages), messages)
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "Svar kort.") {
		t.Fatalf("system message = %+v", messages[0])
	}
	if messages[1].Content != "prior" || messages[2].Content != "current" {
		t.Fatalf("history messages = %+v", messages[1:])
	}
	for _, msg := range messages {
		if msg.Content == "sent after start" {
			t.Fatal("post-start user message was included")
		}
	}
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

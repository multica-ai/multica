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

func TestBuildGatewayIssueMessagesIncludesIssueOpeningAndDistinguishesAuthors(t *testing.T) {
	started := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	agentID := uuid(1)
	otherAgentID := uuid(2)
	memberID := uuid(3)

	issue := db.Issue{
		Title:       "Cloud runtime: claim issue-tasks",
		Description: pgtype.Text{String: "Need to widen the claim from chat-only.", Valid: true},
	}
	comments := []db.Comment{
		{ID: uuid(10), AuthorType: "member", AuthorID: memberID, Content: "Any update?", CreatedAt: ts(started.Add(-3 * time.Minute))},
		{ID: uuid(11), AuthorType: "agent", AuthorID: agentID, Content: "Looking into it.", CreatedAt: ts(started.Add(-2 * time.Minute))},
		{ID: uuid(12), AuthorType: "agent", AuthorID: otherAgentID, Content: "I can help too.", CreatedAt: ts(started.Add(-90 * time.Second))},
		{ID: uuid(13), AuthorType: "member", AuthorID: memberID, Content: "Posted after task start.", CreatedAt: ts(started.Add(2 * time.Minute))},
	}
	trigger := &db.Comment{ID: uuid(14), AuthorType: "member", AuthorID: memberID, Content: "Please answer this one.", CreatedAt: ts(started.Add(-time.Minute))}

	messages := buildGatewayIssueMessages(db.Agent{
		ID:           agentID,
		Name:         "Sara",
		Instructions: "Be concise.",
	}, issue, comments, trigger, ts(started), 50)

	if messages[0].Role != "system" {
		t.Fatalf("expected system first, got %+v", messages[0])
	}
	if !strings.Contains(messages[0].Content, "Be concise.") {
		t.Fatalf("system prompt missing agent instructions: %s", messages[0].Content)
	}
	if messages[1].Role != "user" || !strings.Contains(messages[1].Content, "Cloud runtime: claim issue-tasks") {
		t.Fatalf("expected issue opening as user message, got %+v", messages[1])
	}
	if !strings.Contains(messages[1].Content, "widen the claim from chat-only") {
		t.Fatalf("issue opening missing description: %s", messages[1].Content)
	}

	want := []GatewayMessage{
		{Role: "user", Content: "Any update?"},
		{Role: "assistant", Content: "Looking into it."},
		{Role: "user", Content: "I can help too."},
		{Role: "user", Content: "Please answer this one."},
	}
	got := messages[2:]
	if len(got) != len(want) {
		t.Fatalf("transcript len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transcript[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBuildGatewayIssueMessagesAppliesLimitButKeepsTriggerComment(t *testing.T) {
	started := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	agentID := uuid(1)
	memberID := uuid(2)

	issue := db.Issue{Title: "Issue"}
	comments := []db.Comment{
		{ID: uuid(10), AuthorType: "member", AuthorID: memberID, Content: "one", CreatedAt: ts(started.Add(-5 * time.Minute))},
		{ID: uuid(11), AuthorType: "member", AuthorID: memberID, Content: "two", CreatedAt: ts(started.Add(-4 * time.Minute))},
		{ID: uuid(12), AuthorType: "member", AuthorID: memberID, Content: "three", CreatedAt: ts(started.Add(-3 * time.Minute))},
	}
	trigger := &db.Comment{ID: uuid(13), AuthorType: "member", AuthorID: memberID, Content: "trigger"}

	messages := buildGatewayIssueMessages(db.Agent{ID: agentID, Name: "Sara"}, issue, comments, trigger, ts(started), 2)

	// Expect: system + issue opening + last 2 transcript entries + trigger.
	if len(messages) != 5 {
		t.Fatalf("len = %d, want 5: %+v", len(messages), messages)
	}
	if messages[2].Content != "two" || messages[3].Content != "three" {
		t.Fatalf("limited transcript = %+v", messages[2:4])
	}
	if messages[4].Content != "trigger" {
		t.Fatalf("trigger comment dropped: %+v", messages[4])
	}
}

func TestBuildGatewayAutopilotMessagesEmbedsTitleAndDescription(t *testing.T) {
	ap := db.Autopilot{
		Title:       "Daily digest",
		Description: pgtype.Text{String: "Summarize yesterday's commits.", Valid: true},
	}
	messages := buildGatewayAutopilotMessages(db.Agent{Name: "Sara", Instructions: "Tone: terse."}, ap)

	if len(messages) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(messages), messages)
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "Tone: terse.") {
		t.Fatalf("system prompt = %+v", messages[0])
	}
	if messages[1].Role != "user" {
		t.Fatalf("expected user role: %+v", messages[1])
	}
	if !strings.Contains(messages[1].Content, "Daily digest") || !strings.Contains(messages[1].Content, "Summarize yesterday's commits.") {
		t.Fatalf("user message missing autopilot context: %s", messages[1].Content)
	}
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func uuid(n byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[15] = n
	return u
}

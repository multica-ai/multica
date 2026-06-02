package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStripMentionInjectionsAgent(t *testing.T) {
	input := `Error in [Malicious](mention://agent/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	got := stripMentionInjections(input)
	want := `Error in [Malicious] (mention://agent/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	if got != want {
		t.Fatalf("stripMentionInjections:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripMentionInjectionsMember(t *testing.T) {
	input := `[Attacker](mention://member/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	got := stripMentionInjections(input)
	want := `[Attacker] (mention://member/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	if got != want {
		t.Fatalf("stripMentionInjections:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripMentionInjectionsIssue(t *testing.T) {
	input := `See [MIN-72](mention://issue/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	got := stripMentionInjections(input)
	if got != input {
		t.Fatalf("stripMentionInjections should preserve issue mentions:\ngot:  %q\nwant: %q", got, input)
	}
}

func TestStripMentionInjectionsSquad(t *testing.T) {
	input := `[Squad](mention://squad/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	got := stripMentionInjections(input)
	want := `[Squad] (mention://squad/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)`
	if got != want {
		t.Fatalf("stripMentionInjections:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestParseIssueMetadataNil(t *testing.T) {
	got := parseIssueMetadata(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestParseIssueMetadataEmpty(t *testing.T) {
	got := parseIssueMetadata([]byte{})
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestParseIssueMetadataValid(t *testing.T) {
	data := []byte(`{"failure_escalated_task_id": "abc-123"}`)
	got := parseIssueMetadata(data)
	if got["failure_escalated_task_id"] != "abc-123" {
		t.Fatalf("expected abc-123, got %v", got["failure_escalated_task_id"])
	}
}

func TestRecommendRoute(t *testing.T) {
	tests := []struct {
		reason string
		check  func(string) bool
	}{
		{"idle_watchdog", func(s string) bool { return len(s) > 20 && containsStr(s, "reassign") }},
		{"timeout", func(s string) bool { return len(s) > 20 && containsStr(s, "retry") }},
		{"provider_error", func(s string) bool { return len(s) > 20 && containsStr(s, "DevOps") }},
		{"agent_error", func(s string) bool { return len(s) > 20 && containsStr(s, "Spark") }},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := recommendRoute(tt.reason)
			if !tt.check(got) {
				t.Errorf("recommendRoute(%q) = %q, doesn't satisfy predicate", tt.reason, got)
			}
		})
	}
}

func TestRecommendNextHandler(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"idle_watchdog", "QA / Issue Owner"},
		{"timeout", "Team Lead"},
		{"provider_error", "Runtime QA / DevOps"},
		{"permission_error", "DevOps"},
		{"qa_fail", "Implementation Owner"},
		{"block", "Team Lead"},
		{"agent_error", "Spark Preflight / QA"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := recommendNextHandler(tt.reason)
			if got != tt.want {
				t.Errorf("recommendNextHandler(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

func TestHasCompletedTaskAfter(t *testing.T) {
	t.Run("no issue id returns false", func(t *testing.T) {
		svc := &TaskService{Bus: events.New()}
		task := db.AgentTaskQueue{
			CompletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}
		if svc.hasCompletedTaskAfter(context.Background(), task) {
			t.Error("expected false when no issue id")
		}
	})
	t.Run("no completed at returns false", func(t *testing.T) {
		svc := &TaskService{Bus: events.New()}
		task := db.AgentTaskQueue{
			IssueID: testUUID(1),
		}
		if svc.hasCompletedTaskAfter(context.Background(), task) {
			t.Error("expected false when no completed_at")
		}
	})
}

func TestBuildEscalationComment(t *testing.T) {
	svc := &TaskService{}

	now := time.Now()
	task := db.AgentTaskQueue{
		ID:            testUUID(1),
		AgentID:       testUUID(2),
		IssueID:       testUUID(3),
		Attempt:       2,
		MaxAttempts:   3,
		FailureReason: pgtype.Text{String: "timeout", Valid: true},
		Error:         pgtype.Text{String: "operation timed out after 30s", Valid: true},
		CompletedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	}
	issue := db.Issue{
		ID:     testUUID(3),
		Status: "in_progress",
		Title:  "Test issue",
	}

	body := svc.buildEscalationComment(task, issue, "timeout", "operation timed out after 30s")

	checks := []string{
		"**Task Failure Escalation**",
		"**Failed Agent**",
		"**Task ID**",
		"**Failure Reason**",
		"`timeout`",
		"**Attempt**: 2 / 3",
		"timed out",
		"**Recommended Next Step**",
		"Team Lead",
		"automated escalation",
		"Do not @mention",
	}
	for _, c := range checks {
		if !containsStr(body, c) {
			t.Errorf("expected comment body to contain %q", c)
		}
	}
	if containsStr(body, "mention://agent") || containsStr(body, "mention://member") {
		t.Error("comment body contains raw mention links")
	}
}

func TestBuildEscalationCommentTruncatedError(t *testing.T) {
	svc := &TaskService{}

	longErr := ""
	for i := 0; i < 600; i++ {
		longErr += "x"
	}

	task := db.AgentTaskQueue{
		ID:      testUUID(1),
		AgentID: testUUID(2),
		IssueID: testUUID(3),
	}
	issue := db.Issue{ID: testUUID(3), Status: "todo"}

	body := svc.buildEscalationComment(task, issue, "agent_error", longErr)

	if !containsStr(body, "…") {
		t.Errorf("expected truncated error to end with ...\nbody: %s", body)
	}
}

func TestBuildEscalationCommentNoAttempt(t *testing.T) {
	svc := &TaskService{}

	task := db.AgentTaskQueue{
		ID:      testUUID(1),
		AgentID: testUUID(2),
		IssueID: testUUID(3),
		Attempt: 0,
	}
	issue := db.Issue{ID: testUUID(3), Status: "todo"}

	body := svc.buildEscalationComment(task, issue, "agent_error", "")
	if containsStr(body, "Attempt") {
		t.Error("body should not mention attempt when it's zero")
	}
}

// containsStr is a simple substring check.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

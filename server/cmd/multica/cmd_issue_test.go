package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"short", "abc", "abc"},
		{"exact 8", "abcdefgh", "abcdefgh"},
		{"longer than 8", "abcdefgh-1234-5678", "abcdefgh"},
		{"empty", "", ""},
		{"unicode", "日本語テスト文字列追加", "日本語テスト文字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.id)
			if got != tt.want {
				t.Errorf("truncateID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestFormatAssignee(t *testing.T) {
	tests := []struct {
		name  string
		issue map[string]any
		want  string
	}{
		{"empty", map[string]any{}, ""},
		{"no type", map[string]any{"assignee_id": "abc"}, ""},
		{"no id", map[string]any{"assignee_type": "member"}, ""},
		{"member", map[string]any{"assignee_type": "member", "assignee_id": "abcdefgh-1234"}, "member:abcdefgh"},
		{"agent", map[string]any{"assignee_type": "agent", "assignee_id": "xyz"}, "agent:xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAssignee(tt.issue)
			if got != tt.want {
				t.Errorf("formatAssignee() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAssignee(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "user-1111", "name": "Alice Smith"},
		{"user_id": "user-2222", "name": "Bob Jones"},
	}
	agentsResp := []map[string]any{
		{"id": "agent-3333", "name": "CodeBot"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("exact match member", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "Alice Smith")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-1111" {
			t.Errorf("got (%q, %q), want (member, user-1111)", aType, aID)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-2222" {
			t.Errorf("got (%q, %q), want (member, user-2222)", aType, aID)
		}
	})

	t.Run("match agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "codebot")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "agent-3333" {
			t.Errorf("got (%q, %q), want (agent, agent-3333)", aType, aID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, _, err := resolveAssignee(ctx, client, "nobody")
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		// Both "Alice Smith" and "Bob Jones" contain a space — but let's use a broader query
		// "e" matches "Alice Smith" and "Bob Jones" and "CodeBot"
		_, _, err := resolveAssignee(ctx, client, "o")
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		if got := err.Error(); !strings.Contains(got, "ambiguous") {
			t.Errorf("expected ambiguous error, got: %s", got)
		}
	})

	t.Run("missing workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, _, err := resolveAssignee(ctx, noWSClient, "alice")
		if err == nil {
			t.Fatal("expected error for missing workspace ID")
		}
	})
}

func TestIssueDisplayID(t *testing.T) {
	tests := []struct {
		name  string
		issue map[string]any
		want  string
	}{
		{
			name:  "returns identifier when present",
			issue: map[string]any{"identifier": "PRA-42", "id": "5847db82-bf0a-404f-b987-5cfc2d402ebd"},
			want:  "PRA-42",
		},
		{
			name:  "falls back to truncated UUID when identifier is absent",
			issue: map[string]any{"id": "5847db82-bf0a-404f-b987-5cfc2d402ebd"},
			want:  "5847db82",
		},
		{
			name:  "falls back to truncated UUID when identifier is empty string",
			issue: map[string]any{"identifier": "", "id": "5847db82-bf0a-404f-b987-5cfc2d402ebd"},
			want:  "5847db82",
		},
		{
			name:  "empty map returns empty string",
			issue: map[string]any{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueDisplayID(tt.issue)
			if got != tt.want {
				t.Errorf("issueDisplayID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIssueListUsesIdentifierAsID(t *testing.T) {
	issues := []map[string]any{
		{"identifier": "MUL-1", "id": "aaaaaaaa-0000-0000-0000-000000000001", "title": "First", "status": "todo", "priority": "high"},
		{"identifier": "MUL-2", "id": "bbbbbbbb-0000-0000-0000-000000000002", "title": "Second", "status": "backlog", "priority": "none"},
		{"id": "cccccccc-0000-0000-0000-000000000003", "title": "No identifier", "status": "done", "priority": "low"},
	}

	rows := make([][]string, 0, len(issues))
	for _, issue := range issues {
		rows = append(rows, []string{
			issueDisplayID(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
		})
	}

	// Verify identifiers are used where available, truncated UUID otherwise
	if rows[0][0] != "MUL-1" {
		t.Errorf("row 0 ID = %q, want MUL-1", rows[0][0])
	}
	if rows[1][0] != "MUL-2" {
		t.Errorf("row 1 ID = %q, want MUL-2", rows[1][0])
	}
	if rows[2][0] != "cccccccc" {
		t.Errorf("row 2 ID = %q, want cccccccc (truncated UUID fallback)", rows[2][0])
	}
}

func TestValidIssueStatuses(t *testing.T) {
	expected := map[string]bool{
		"backlog":     true,
		"todo":        true,
		"in_progress": true,
		"in_review":   true,
		"done":        true,
		"blocked":     true,
		"cancelled":   true,
	}
	for _, s := range validIssueStatuses {
		if !expected[s] {
			t.Errorf("unexpected status in validIssueStatuses: %q", s)
		}
	}
	if len(validIssueStatuses) != len(expected) {
		t.Errorf("validIssueStatuses has %d entries, expected %d", len(validIssueStatuses), len(expected))
	}
}


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

func TestIssueSubscriberList(t *testing.T) {
	subscribersResp := []map[string]any{
		{
			"issue_id":   "issue-1",
			"user_type":  "member",
			"user_id":    "user-1111",
			"reason":     "creator",
			"created_at": "2026-04-01T10:00:00Z",
		},
		{
			"issue_id":   "issue-1",
			"user_type":  "agent",
			"user_id":    "agent-3333",
			"reason":     "manual",
			"created_at": "2026-04-01T11:00:00Z",
		},
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(subscribersResp)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	var got []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/issue-1/subscribers", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if gotPath != "/api/issues/issue-1/subscribers" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(got))
	}
	if got[0]["user_type"] != "member" || got[1]["user_type"] != "agent" {
		t.Errorf("unexpected subscriber ordering: %+v", got)
	}
}

func TestIssueSubscriberMutationBody(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		user     string
		members  []map[string]any
		agents   []map[string]any
		wantPath string
		wantBody map[string]any
	}{
		{
			name:     "subscribe caller (no user flag)",
			action:   "subscribe",
			user:     "",
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{},
		},
		{
			name:     "unsubscribe caller",
			action:   "unsubscribe",
			user:     "",
			wantPath: "/api/issues/issue-1/unsubscribe",
			wantBody: map[string]any{},
		},
		{
			name:     "subscribe a member by name",
			action:   "subscribe",
			user:     "alice",
			members:  []map[string]any{{"user_id": "user-1111", "name": "Alice Smith"}},
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{"user_type": "member", "user_id": "user-1111"},
		},
		{
			name:     "subscribe an agent by name",
			action:   "subscribe",
			user:     "codebot",
			agents:   []map[string]any{{"id": "agent-3333", "name": "CodeBot"}},
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{"user_type": "agent", "user_id": "agent-3333"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/workspaces/ws-1/members":
					json.NewEncoder(w).Encode(tt.members)
					return
				case "/api/agents":
					json.NewEncoder(w).Encode(tt.agents)
					return
				}
				gotPath = r.URL.Path
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				json.NewDecoder(r.Body).Decode(&gotBody)
				json.NewEncoder(w).Encode(map[string]bool{"subscribed": tt.action == "subscribe"})
			}))
			defer srv.Close()

			client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
			ctx := context.Background()

			body := map[string]any{}
			if tt.user != "" {
				uType, uID, err := resolveAssignee(ctx, client, tt.user)
				if err != nil {
					t.Fatalf("resolveAssignee: %v", err)
				}
				body["user_type"] = uType
				body["user_id"] = uID
			}

			var result map[string]any
			path := "/api/issues/issue-1/" + tt.action
			if err := client.PostJSON(ctx, path, body, &result); err != nil {
				t.Fatalf("PostJSON: %v", err)
			}

			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			for k, want := range tt.wantBody {
				if gotBody[k] != want {
					t.Errorf("body[%q] = %v, want %v", k, gotBody[k], want)
				}
			}
			if len(tt.wantBody) == 0 && len(gotBody) != 0 {
				t.Errorf("expected empty body, got %+v", gotBody)
			}
		})
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

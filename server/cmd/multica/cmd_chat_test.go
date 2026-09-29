package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const testChatSessionUUID = "11111111-1111-1111-1111-111111111111"

func newChatSessionsTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions"}
	cmd.Flags().String("status", "active", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newChatMessagesTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "messages"}
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().String("before-created-at", "", "")
	cmd.Flags().String("before-id", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func chatSessionFixture(status string) map[string]any {
	return map[string]any{
		"id":         testChatSessionUUID,
		"status":     status,
		"updated_at": "2026-09-29T06:00:00Z",
		"created_at": "2026-09-29T05:00:00Z",
		"agent_id":   "22222222-2222-2222-2222-222222222222",
		"creator_id": "33333333-3333-3333-3333-333333333333",
	}
}

func TestChatCommandsRegistered(t *testing.T) {
	commands := map[string]*cobra.Command{}
	for _, cmd := range chatCmd.Commands() {
		commands[cmd.Name()] = cmd
	}
	for _, name := range []string{"history", "thread", "sessions", "messages"} {
		if commands[name] == nil {
			t.Fatalf("chat command %q is not registered", name)
		}
	}
	if chatSessionsCmd.Flag("status") == nil || chatSessionsCmd.Flag("output") == nil {
		t.Fatal("chat sessions flags are not registered")
	}
	for _, name := range []string{"limit", "before-created-at", "before-id", "output"} {
		if chatMessagesCmd.Flag(name) == nil {
			t.Fatalf("chat messages flag %q is not registered", name)
		}
	}
}

func TestRunChatSessionsFiltersStatusAndEncodesQuery(t *testing.T) {
	sessions := []map[string]any{chatSessionFixture("active"), chatSessionFixture("archived")}
	for _, tc := range []struct {
		name       string
		status     string
		query      string
		wantStatus []string
	}{
		{name: "active", status: "active", query: "active", wantStatus: []string{"active"}},
		{name: "archived", status: "archived", query: "all", wantStatus: []string{"archived"}},
		{name: "all", status: "all", query: "all", wantStatus: []string{"active", "archived"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/chat/sessions" {
					t.Fatalf("path = %q, want /api/chat/sessions", r.URL.Path)
				}
				gotQuery = r.URL.Query().Get("status")
				_ = json.NewEncoder(w).Encode(sessions)
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_test-token")

			cmd := newChatSessionsTestCmd()
			_ = cmd.Flags().Set("status", tc.status)
			out, err := captureStdout(t, func() error { return runChatSessions(cmd, nil) })
			if err != nil {
				t.Fatalf("runChatSessions: %v", err)
			}
			if gotQuery != tc.query {
				t.Fatalf("status query = %q, want %q", gotQuery, tc.query)
			}

			var got []map[string]any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("decode output: %v\n%s", err, out)
			}
			if len(got) != len(tc.wantStatus) {
				t.Fatalf("session count = %d, want %d", len(got), len(tc.wantStatus))
			}
			for i, want := range tc.wantStatus {
				if got[i]["status"] != want {
					t.Errorf("session[%d].status = %v, want %s", i, got[i]["status"], want)
				}
			}
		})
	}
}

func TestRunChatSessionsRejectsInvalidStatus(t *testing.T) {
	cmd := newChatSessionsTestCmd()
	_ = cmd.Flags().Set("status", "deleted")
	if err := runChatSessions(cmd, nil); err == nil || !strings.Contains(err.Error(), "invalid chat session status") {
		t.Fatalf("runChatSessions error = %v, want invalid status", err)
	}
}

func TestRunChatSessionsPropagatesServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newChatSessionsTestCmd()
	out, err := captureStdout(t, func() error { return runChatSessions(cmd, nil) })
	if err == nil {
		t.Fatal("runChatSessions: expected server error")
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty output on server error", out)
	}
}

func TestRunChatMessagesPaginatesWithRawCursorAndFixedJSON(t *testing.T) {
	var requests int
	var gotBeforeCreatedAt, gotBeforeID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/sessions/"+testChatSessionUUID+"/messages/page" {
			t.Fatalf("request = %s %s, want chat message page", r.Method, r.URL.Path)
		}
		requests++
		if requests == 1 {
			if r.URL.Query().Get("limit") != "2" {
				t.Errorf("first limit = %q, want 2", r.URL.Query().Get("limit"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{"id": "m1", "created_at": "2026-09-29T05:00:00.123456789Z", "content": "old"}},
				"limit":    2, "has_more": true,
				"next_cursor": map[string]string{
					"created_at": "2026-09-29T05:00:00.123456789+00:00",
					"id":         "44444444-4444-4444-4444-444444444444",
				},
			})
			return
		}
		gotBeforeCreatedAt = r.URL.Query().Get("before_created_at")
		gotBeforeID = r.URL.Query().Get("before_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{{"id": "m0", "created_at": "2026-09-29T04:00:00Z", "content": "oldest"}},
			"limit":    2, "has_more": false,
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	first := newChatMessagesTestCmd()
	_ = first.Flags().Set("limit", "2")
	out, err := captureStdout(t, func() error { return runChatMessages(first, []string{testChatSessionUUID}) })
	if err != nil {
		t.Fatalf("first runChatMessages: %v", err)
	}
	var firstJSON map[string]any
	if err := json.Unmarshal([]byte(out), &firstJSON); err != nil {
		t.Fatalf("decode first output: %v\n%s", err, out)
	}
	assertChatMessagePageKeys(t, firstJSON)
	cursor := firstJSON["next_cursor"].(map[string]any)

	second := newChatMessagesTestCmd()
	_ = second.Flags().Set("limit", "2")
	_ = second.Flags().Set("before-created-at", cursor["created_at"].(string))
	_ = second.Flags().Set("before-id", cursor["id"].(string))
	out, err = captureStdout(t, func() error { return runChatMessages(second, []string{testChatSessionUUID}) })
	if err != nil {
		t.Fatalf("second runChatMessages: %v", err)
	}
	var secondJSON map[string]any
	if err := json.Unmarshal([]byte(out), &secondJSON); err != nil {
		t.Fatalf("decode second output: %v\n%s", err, out)
	}
	assertChatMessagePageKeys(t, secondJSON)
	if gotBeforeCreatedAt != "2026-09-29T05:00:00.123456789+00:00" || gotBeforeID != "44444444-4444-4444-4444-444444444444" {
		t.Fatalf("second cursor = %q/%q, want raw first cursor", gotBeforeCreatedAt, gotBeforeID)
	}
	if secondJSON["next_cursor"] != nil {
		t.Fatalf("terminal next_cursor = %#v, want null", secondJSON["next_cursor"])
	}
}

func assertChatMessagePageKeys(t *testing.T, payload map[string]any) {
	t.Helper()
	want := map[string]bool{"messages": true, "limit": true, "has_more": true, "next_cursor": true}
	if len(payload) != len(want) {
		t.Fatalf("JSON keys = %#v, want exactly messages/limit/has_more/next_cursor", payload)
	}
	for key := range payload {
		if !want[key] {
			t.Fatalf("unexpected JSON key %q", key)
		}
	}
}

func TestRunChatMessagesRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		set  func(*cobra.Command)
		args []string
		want string
	}{
		{name: "bad session id", args: []string{"not-a-uuid"}, want: "invalid chat session ID"},
		{name: "bad limit", set: func(cmd *cobra.Command) { _ = cmd.Flags().Set("limit", "0") }, args: []string{testChatSessionUUID}, want: "invalid chat message limit"},
		{name: "partial cursor", set: func(cmd *cobra.Command) { _ = cmd.Flags().Set("before-id", testChatSessionUUID) }, args: []string{testChatSessionUUID}, want: "must be provided together"},
		{name: "bad timestamp", set: func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("before-created-at", "tomorrow")
			_ = cmd.Flags().Set("before-id", testChatSessionUUID)
		}, args: []string{testChatSessionUUID}, want: "invalid --before-created-at"},
		{name: "table output", set: func(cmd *cobra.Command) { _ = cmd.Flags().Set("output", "table") }, args: []string{testChatSessionUUID}, want: "only support --output json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newChatMessagesTestCmd()
			if tc.set != nil {
				tc.set(cmd)
			}
			err := runChatMessages(cmd, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunChatMessagesPropagatesServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newChatMessagesTestCmd()
	out, err := captureStdout(t, func() error { return runChatMessages(cmd, []string{testChatSessionUUID}) })
	if err == nil {
		t.Fatal("runChatMessages: expected server error")
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty output on server error", out)
	}
}

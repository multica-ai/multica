package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

func newChatListTestCmd(status string, limit int, output string) *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("status", status, "")
	cmd.Flags().Int("limit", limit, "")
	cmd.Flags().String("output", output, "")
	return cmd
}

func newChatArchiveTestCmd(dryRun bool, output string) *cobra.Command {
	cmd := &cobra.Command{Use: "archive"}
	cmd.Flags().Bool("dry-run", dryRun, "")
	cmd.Flags().String("output", output, "")
	return cmd
}

func TestChatListArchivedKeepsOnlyArchivedSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat/sessions" || r.URL.Query().Get("status") != "all" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "s-1", "title": "Active", "status": "active"},
			{"id": "s-2", "title": "Old", "status": "archived"},
			{"id": "s-3", "title": "Older", "status": "archived"},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error { return runChatList(newChatListTestCmd("archived", 1, "json"), nil) })
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["id"] != "s-2" {
		t.Fatalf("sessions = %v, want only s-2 (archived, limited to 1)", got)
	}
}

func TestChatListActiveAsksForTheDefaultList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("active list should not pass a status, got %q", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode([]map[string]any{{
			"id": "s-1", "title": "Release notes", "status": "active", "unread_count": 2,
			"updated_at": "2026-11-20T00:00:00Z", "last_message": map[string]any{"created_at": "2026-11-23T09:00:00Z"},
		}})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error { return runChatList(newChatListTestCmd("active", 0, "table"), nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"s-1", "Release notes", "active", "2", "2026-11-23T09:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestChatListRejectsUnknownStatus(t *testing.T) {
	err := runChatList(newChatListTestCmd("deleted", 0, "json"), nil)
	if err == nil || !strings.Contains(err.Error(), "--status") {
		t.Fatalf("err = %v, want a --status error", err)
	}
}

func TestChatArchiveReportsEveryIDAndFailsWhenAnyFails(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/archive") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/chat/sessions/"), "/archive")
		mu.Lock()
		bodies = append(bodies, id)
		mu.Unlock()
		if body["archived"] != true {
			t.Errorf("archived = %v, want true", body["archived"])
		}
		if id == "missing" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "chat session not found"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "archived", "title": "T " + id})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runChatSetArchived(newChatArchiveTestCmd(false, "json"), []string{"s-1", "missing", "s-2", "s-1"}, true)
	})
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("err = %v, want 1 of 3 failed", err)
	}
	var got []chatArchiveResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Status != "archived" || got[1].Error == "" || got[2].ID != "s-2" || got[2].Status != "archived" {
		t.Fatalf("results = %+v", got)
	}
	if strings.Join(bodies, ",") != "s-1,missing,s-2" {
		t.Fatalf("requests = %v, want each id once in order", bodies)
	}
}

func TestChatRestoreSendsArchivedFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["archived"] != false {
			t.Errorf("archived = %v, want false", body["archived"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "s-1", "status": "active", "title": "Back"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runChatSetArchived(newChatArchiveTestCmd(false, "table"), []string{"s-1"}, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s-1: active") {
		t.Fatalf("output = %q", out)
	}
}

func TestChatArchiveDryRunWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry run sent a request: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runChatSetArchived(newChatArchiveTestCmd(true, "table"), []string{"s-1", "s-2"}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s-1: would archive") || !strings.Contains(out, "s-2: would archive") {
		t.Fatalf("output = %q", out)
	}
}

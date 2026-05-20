package main

// CEREBRO-PATCH(notifications-cli): JEH-1807 notification center CLI tests.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func freshNotificationsListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	registerNotificationsListFlags(cmd)
	return cmd
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = r.Close()
	}()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	return buf.String(), runErr
}

func TestRunNotificationsListJSONUsesNotificationsEndpoint(t *testing.T) {
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	var gotPath string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		if r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Errorf("X-Workspace-ID = %q, want ws-1", r.Header.Get("X-Workspace-ID"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":          "notif-1",
				"type":        "mention",
				"issue_title": "Build B4",
				"excerpt":     "Please take a look",
				"created_at":  "2026-05-19T13:00:00Z",
				"read_at":     nil,
			},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := freshNotificationsListCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("limit", "7")
	_ = cmd.Flags().Set("unread", "true")

	out, err := captureStdout(t, func() error {
		return runNotificationsList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runNotificationsList: %v", err)
	}
	if gotPath != "/api/notifications" {
		t.Fatalf("path = %q, want /api/notifications", gotPath)
	}
	if !strings.Contains(gotQuery, "limit=7") || !strings.Contains(gotQuery, "unread=true") {
		t.Fatalf("query = %q, want limit=7 and unread=true", gotQuery)
	}

	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("json output did not decode: %v\n%s", err, out)
	}
	if decoded[0]["issue_title"] != "Build B4" {
		t.Fatalf("JSON output should preserve API field names, got %+v", decoded[0])
	}
	if _, ok := decoded[0]["read_at"]; !ok {
		t.Fatalf("JSON output should preserve read_at field, got %+v", decoded[0])
	}
}

func TestRunNotificationsListTableShowsExpectedColumns(t *testing.T) {
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"type":        "agent.run_failed",
				"issue_title": "Failing task",
				"excerpt":     "The run exited with status 1 after a long error message that should be shortened in table output.",
				"created_at":  "2026-05-19T13:00:00Z",
				"read_at":     "2026-05-19T13:01:00Z",
			},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := freshNotificationsListCmd()
	out, err := captureStdout(t, func() error {
		return runNotificationsList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runNotificationsList: %v", err)
	}
	for _, want := range []string{"TYPE", "ISSUE TITLE", "EXCERPT", "CREATED_AT", "READ_AT", "agent.run_failed", "Failing task"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

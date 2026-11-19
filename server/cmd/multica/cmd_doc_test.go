package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestDocCommandsExposeOnlyAgentReadWrite(t *testing.T) {
	expected := map[string]bool{"list": true, "search": true, "get": true, "create": true, "update": true}
	for _, command := range docCmd.Commands() {
		if !expected[command.Name()] {
			t.Fatalf("unexpected doc command: %s", command.Name())
		}
		delete(expected, command.Name())
	}
	if len(expected) != 0 {
		t.Fatalf("missing commands: %v", expected)
	}
}

func TestDocConflictErrorExplainsRecovery(t *testing.T) {
	err := docWriteError("update doc", 3, &cli.HTTPError{StatusCode: http.StatusConflict, Body: `{"code":"revision_conflict","actual_revision":4}`})
	if !strings.Contains(err.Error(), "nothing was written") || !strings.Contains(err.Error(), "--base-revision") {
		t.Fatalf("missing recovery: %v", err)
	}
	cause := errors.New("connection closed")
	if !errors.Is(docWriteError("update doc", 3, cause), cause) {
		t.Fatal("lost underlying error")
	}
}

func TestRunDocSearchQueriesServerSearch(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{map[string]any{
			"id": "doc-1", "title": "Release runbook", "updated_at": "2026-11-19T00:00:00Z",
			"match_source": "content", "matched_snippet": "...rotate the credentials before cutover...",
		}}})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := docSearchCmd
	_ = cmd.Flags().Set("limit", "5")
	t.Cleanup(func() { _ = cmd.Flags().Set("limit", "20") })
	out, err := captureStdout(t, func() error { return runDocSearch(cmd, []string{"rotate credentials"}) })
	if err != nil {
		t.Fatalf("runDocSearch: %v", err)
	}
	if gotPath != "/api/docs/search" || gotQuery.Get("q") != "rotate credentials" || gotQuery.Get("limit") != "5" {
		t.Fatalf("request = %s?%s", gotPath, gotQuery.Encode())
	}
	if !strings.Contains(out, "doc-1") || !strings.Contains(out, "content: ...rotate the credentials") {
		t.Fatalf("table output missing the hit: %q", out)
	}
}

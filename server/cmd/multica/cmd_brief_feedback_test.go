package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newBriefFeedbackTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "feedback"}
	addBriefFeedbackFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func TestParseBriefRecentDays(t *testing.T) {
	for in, want := range map[string]int{"1d": 1, "14d": 14, "35d": 35, " 7d ": 7} {
		got, err := parseBriefRecentDays(in)
		if err != nil || got != want {
			t.Errorf("parseBriefRecentDays(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "14", "0d", "36d", "014d", "2w", "24h", "1.5d", "-1d", "14D", "d", "14 d"} {
		if got, err := parseBriefRecentDays(bad); err == nil {
			t.Errorf("parseBriefRecentDays(%q) = %d, want an error", bad, got)
		}
	}
}

func TestBriefFeedbackSendsTheWindowAndPrintsJSON(t *testing.T) {
	var gotPath, gotDays string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotDays = r.URL.Query().Get("days")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"rating": "not_useful", "tags": []string{"too_long"}, "note": "shorter",
			"brief_kind": "daily", "brief_created_at": "2026-11-01T08:00:00Z",
		}})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runBriefFeedback(newBriefFeedbackTestCmd(t, "--recent", "14d", "--json"), nil)
	})
	if err != nil {
		t.Fatalf("runBriefFeedback: %v", err)
	}
	if gotPath != "/api/briefs/feedback/recent" || gotDays != "14" {
		t.Fatalf("request = %s days=%s, want GET /api/briefs/feedback/recent days=14", gotPath, gotDays)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out)
	}
	if len(entries) != 1 || entries[0]["rating"] != "not_useful" || entries[0]["brief_created_at"] != "2026-11-01T08:00:00Z" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestBriefFeedbackDefaultsToFourteenDays(t *testing.T) {
	var gotDays string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDays = r.URL.Query().Get("days")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	if _, err := captureStdout(t, func() error {
		return runBriefFeedback(newBriefFeedbackTestCmd(t, "--json"), nil)
	}); err != nil {
		t.Fatalf("runBriefFeedback: %v", err)
	}
	if gotDays != "14" {
		t.Fatalf("days = %q, want 14", gotDays)
	}
}

func TestBriefFeedbackRejectsABadWindowBeforeCallingTheServer(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runBriefFeedback(newBriefFeedbackTestCmd(t, "--recent", "2w"), nil)
	if err == nil || !strings.Contains(err.Error(), `--recent "2w"`) {
		t.Fatalf("error = %v, want a refusal naming --recent", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server was called %d times for a refused window", calls.Load())
	}
}

func TestBriefFeedbackShowsTheServersRefusal(t *testing.T) {
	const message = "only the workspace's secretary agent or an admin member may read brief feedback"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runBriefFeedback(newBriefFeedbackTestCmd(t, "--json"), nil)
	if err == nil || !strings.Contains(cli.FormatError(err, false), message) {
		t.Fatalf("the server's refusal should reach the caller unchanged, got %v", err)
	}
}

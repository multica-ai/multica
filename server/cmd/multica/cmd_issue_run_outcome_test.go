package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestIssueRunOutcomeNote(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome *issueRunOutcome
		noStart bool
		want    string
	}{
		{name: "older server", outcome: nil, want: ""},
		{name: "run started", outcome: &issueRunOutcome{Status: "queued", ReasonCode: "queued"}, want: ""},
		{name: "pending run covers it", outcome: &issueRunOutcome{Status: "coalesced", ReasonCode: "coalesced"}, want: "no new run started: a pending run on this issue already covers it"},
		{name: "active run covers it", outcome: &issueRunOutcome{Status: "deferred", ReasonCode: "already_active"}, want: "no new run started: the assignee's active run on this issue covers it"},
		{name: "backlog", outcome: &issueRunOutcome{Status: "deferred", ReasonCode: "deferred"}, want: "no run started: the issue is in backlog and runs once it moves to an active status"},
		{name: "caller asked for no run", outcome: &issueRunOutcome{Status: "deferred", ReasonCode: "deferred"}, noStart: true, want: ""},
		{name: "blocked", outcome: &issueRunOutcome{Status: "blocked", ReasonCode: "issue_in_triage"}, want: "no run started: the issue is in Triage; accept it first (issue_in_triage)"},
		{name: "blocked for a newer reason", outcome: &issueRunOutcome{Status: "blocked", ReasonCode: "quota_exceeded"}, want: "no run started: quota_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := issueRunOutcomeNote(tc.outcome, tc.noStart); got != tc.want {
				t.Fatalf("note = %q, want %q", got, tc.want)
			}
		})
	}
}

// `issue status` tells an agent caller when its write started no run, so it
// does not report the run as started (#10845).
func TestRunIssueStatusReportsWhyNoRunStarted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		runOutcome any
		want       string
	}{
		{name: "blocked", runOutcome: map[string]any{"target_type": "agent", "target_id": "agent-1", "status": "blocked", "reason_code": "issue_in_triage"},
			want: "No run started: the issue is in Triage; accept it first (issue_in_triage).\n"},
		{name: "older server", runOutcome: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1", "status": "backlog"})
				case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-1":
					resp := map[string]any{"id": "issue-1", "identifier": "MUL-1", "status": "todo"}
					if tc.runOutcome != nil {
						resp["run_outcome"] = tc.runOutcome
					}
					_ = json.NewEncoder(w).Encode(resp)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())
			t.Setenv("MULTICA_TOKEN", "mat_test-token")

			stderr := captureStderr(t)
			defer stderr.restore()
			if err := runIssueStatus(newIssueStatusTestCmd(), []string{"MUL-1", "todo"}); err != nil {
				t.Fatalf("runIssueStatus: %v", err)
			}
			report := strings.TrimPrefix(stderr.read(), "Issue MUL-1 status changed to todo.\n")
			if report != tc.want {
				t.Fatalf("stderr after the status line = %q, want %q", report, tc.want)
			}
		})
	}
}

// A batch row with no reason of its own shows why its write started no run,
// and --output json keeps the outcome.
func TestPrintIssueBatchResultsShowsRunOutcome(t *testing.T) {
	var resp issueBatchResponse
	if err := json.Unmarshal([]byte(`{"updated":1,"results":[{"issue_id":"issue-1","identifier":"MUL-1","outcome":"updated",
		"run_outcome":{"target_type":"agent","target_id":"agent-1","status":"blocked","reason_code":"issue_in_triage"}}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"table", "json"} {
		cmd := &cobra.Command{Use: "update"}
		cmd.Flags().String("output", output, "")
		cmd.Flags().Bool("no-start", false, "")
		stdout, err := captureStdout(t, func() error { return printIssueBatchResults(cmd, resp) })
		if err != nil {
			t.Fatal(err)
		}
		want := "no run started: the issue is in Triage; accept it first (issue_in_triage)"
		if output == "json" {
			want = `"reason_code": "issue_in_triage"`
		}
		if !strings.Contains(stdout, want) {
			t.Fatalf("%s output = %q, want it to contain %q", output, stdout, want)
		}
	}
}

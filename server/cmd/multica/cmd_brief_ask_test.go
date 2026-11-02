package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	testSquadID = "11111111-1111-4111-8111-111111111111"
	testIssueID = "22222222-2222-4222-8222-222222222222"
)

func newBriefAskTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "ask"}
	addBriefAskFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func TestBriefAskSendsTheQuestionOnTheIssue(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/briefs/inquiries" {
			t.Errorf("request = %s %s, want POST /api/briefs/inquiries", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "inq-1"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runBriefAsk(newBriefAskTestCmd(t, "--squad", testSquadID, "--issue", testIssueID), []string{"  Why did the pilot slip?  "})
	})
	if err != nil {
		t.Fatalf("runBriefAsk: %v", err)
	}
	want := map[string]any{"squad_id": testSquadID, "issue_id": testIssueID, "question": "Why did the pilot slip?"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%s] = %v, want %v (body %v)", k, got[k], v, got)
		}
	}
	if !strings.Contains(out, "inq-1") {
		t.Errorf("output should name the inquiry: %q", out)
	}
}

func TestBriefAskValidatesFlagsLocally(t *testing.T) {
	cases := map[string][]string{
		"--squad": {"--issue", testIssueID},
		"--issue": {"--squad", testSquadID},
	}
	for want, args := range cases {
		err := runBriefAsk(newBriefAskTestCmd(t, args...), []string{"a question"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("args %v: err = %v, want it to mention %q", args, err, want)
		}
	}
	if err := runBriefAsk(newBriefAskTestCmd(t, "--squad", "not-a-uuid", "--issue", testIssueID), []string{"q"}); err == nil {
		t.Error("a squad id that is not a uuid must be refused before calling the server")
	}
	if err := runBriefAsk(newBriefAskTestCmd(t, "--squad", testSquadID, "--issue", testIssueID), []string{"   "}); err == nil {
		t.Error("an empty question must be refused before calling the server")
	}
}

func TestBriefAskShowsTheDailyLimitRefusal(t *testing.T) {
	const message = "the Secretary has already asked 3 questions of squad leaders today (the limit, reset at midnight UTC)"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "code": "inquiry_limit"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runBriefAsk(newBriefAskTestCmd(t, "--squad", testSquadID, "--issue", testIssueID), []string{"one more"})
	if err == nil || !strings.Contains(cli.FormatError(err, false), "midnight UTC") {
		t.Fatalf("the server's limit message should reach the agent, got %v", err)
	}
}

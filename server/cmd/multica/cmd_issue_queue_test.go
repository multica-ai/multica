package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

const (
	queueIssueA = "11111111-1111-4111-8111-111111111111"
	queueIssueB = "22222222-2222-4222-8222-222222222222"
)

func newIssueQueueTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "queue"}
	cmd.Flags().StringSlice("after", nil, "")
	cmd.Flags().Bool("clear", false, "")
	cmd.Flags().Bool("preview", false, "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func TestRunIssueQueueQueuesWithWaits(t *testing.T) {
	chdirWithDaemonTaskMarker(t)
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": 2})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newIssueQueueTestCmd()
	_ = cmd.Flags().Set("after", queueIssueB)
	out, err := captureStdout(t, func() error {
		return runIssueQueue(cmd, []string{queueIssueA})
	})
	if err != nil {
		t.Fatalf("runIssueQueue: %v", err)
	}
	if gotPath != "/api/issues/batch-run-queue" {
		t.Fatalf("path = %q, want /api/issues/batch-run-queue", gotPath)
	}
	want := map[string]any{
		"issue_ids":       []any{queueIssueA},
		"queued":          true,
		"after_issue_ids": []any{queueIssueB},
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Fatalf("body = %#v, want %#v", gotBody, want)
	}
	if wantOut := "Queued 2 issue(s) to run after 1 issue(s) finish.\n"; out != wantOut {
		t.Fatalf("stdout = %q, want %q", out, wantOut)
	}
}

func TestRunIssueQueueClearSendsNotQueued(t *testing.T) {
	chdirWithDaemonTaskMarker(t)
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": 1})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newIssueQueueTestCmd()
	_ = cmd.Flags().Set("clear", "true")
	if _, err := captureStdout(t, func() error {
		return runIssueQueue(cmd, []string{queueIssueA})
	}); err != nil {
		t.Fatalf("runIssueQueue: %v", err)
	}
	if gotBody["queued"] != false {
		t.Fatalf("queued = %v, want false", gotBody["queued"])
	}
}

func TestRunIssueQueueRejectsBadInputBeforeRequest(t *testing.T) {
	chdirWithDaemonTaskMarker(t)
	cases := map[string]func(cmd *cobra.Command){
		"clear with after": func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("clear", "true")
			_ = cmd.Flags().Set("after", queueIssueB)
		},
		"preview with clear": func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("clear", "true")
			_ = cmd.Flags().Set("preview", "true")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := newIssueQueueTestCmd()
			setup(cmd)
			if err := runIssueQueue(cmd, []string{queueIssueA}); err == nil {
				t.Fatalf("runIssueQueue accepted %s", name)
			}
		})
	}
}

func TestIssueQueueCommandRegistration(t *testing.T) {
	cmd, _, err := issueCmd.Find([]string{"queue"})
	if err != nil {
		t.Fatalf("find issue queue: %v", err)
	}
	if cmd != issueQueueCmd {
		t.Fatalf("found command = %q, want issue queue", cmd.CommandPath())
	}
	for _, flag := range []string{"after", "clear", "preview"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("queue is missing --%s", flag)
		}
	}
}

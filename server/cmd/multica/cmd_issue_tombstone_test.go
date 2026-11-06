package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// tombstoneResponse is the exact JSON shape the detail endpoint answers a
// deleted identifier with (#9070): identifier, number, status, deleted_at —
// and no "id" field, which is what distinguishes a tombstone from a live
// issue whose status happens to be named "deleted".
var tombstoneResponse = map[string]any{
	"identifier": "AF-6",
	"number":     6,
	"status":     "deleted",
	"deleted_at": "2026-10-26T12:00:00Z",
}

// TestResolveIssueRefDeletedTombstone pins the resolver contract for deleted
// identifiers: fetchIssueRef surfaces the tombstone as a *DeletedIssueError
// (carrying the server's JSON) instead of a not-found error, so `issue get`
// can print the machine-readable tombstone and every other issue-aware
// command fails with the real reason.
func TestResolveIssueRefDeletedTombstone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/AF-6" {
			json.NewEncoder(w).Encode(tombstoneResponse)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	_, err := resolveIssueRef(context.Background(), client, "AF-6")
	var deleted *DeletedIssueError
	if !errors.As(err, &deleted) {
		t.Fatalf("expected *DeletedIssueError, got %v", err)
	}
	if got := deleted.Error(); !strings.Contains(got, "AF-6") || !strings.Contains(got, "deleted") {
		t.Fatalf("error should name the identifier and the deletion, got: %s", got)
	}
	if got := strVal(deleted.Detail, "deleted_at"); got != "2026-10-26T12:00:00Z" {
		t.Fatalf("tombstone detail should carry the server payload verbatim, got deleted_at=%q", got)
	}
}

// TestResolveIssueRefLiveIssueWithDeletedStatus pins the false-positive guard:
// a live issue response always carries "id", so a workspace whose status is
// literally named "deleted" must still resolve normally.
func TestResolveIssueRefLiveIssueWithDeletedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/AF-7" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "1881a167-4bb6-4602-944b-f40ce4192fe6",
				"identifier": "AF-7",
				"status":     "deleted",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	got, err := resolveIssueRef(context.Background(), client, "AF-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "1881a167-4bb6-4602-944b-f40ce4192fe6" {
		t.Fatalf("live issue with a 'deleted' status must resolve, got %#v", got)
	}
}

// TestRunIssueGetPrintsTombstoneJSON pins the `issue get --output json`
// contract from #9070: the tombstone reaches the caller as machine-readable
// JSON, distinguishable from a never-issued key (which stays an error)
// without parsing free text.
func TestRunIssueGetPrintsTombstoneJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/AF-6" {
			json.NewEncoder(w).Encode(tombstoneResponse)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	out, err := captureStdout(t, func() error { return runIssueGet(cmd, []string{"AF-6"}) })
	if err != nil {
		t.Fatalf("issue get on a tombstone should succeed, got: %v", err)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("output should be JSON, got: %s", out)
	}
	if printed["status"] != "deleted" || printed["identifier"] != "AF-6" {
		t.Fatalf("tombstone JSON mismatch: %s", out)
	}
}

// TestRunIssueGetNeverIssuedKeyStaysError pins the other half of the #9070
// contract at the CLI: a number that was never issued keeps failing, so the
// two states stay distinguishable without parsing free text.
func TestRunIssueGetNeverIssuedKeyStaysError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	if _, err := captureStdout(t, func() error { return runIssueGet(cmd, []string{"AF-99999"}) }); err == nil {
		t.Fatal("issue get on a never-issued key should keep failing")
	}
}

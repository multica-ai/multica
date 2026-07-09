package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Tests for `multica issue metadata list` 404-degradation behavior, plus
// regression coverage that get / set / delete keep real error semantics so
// we don't lose signal when the user actually depends on the metadata
// endpoint working.
//
// Background: GitHub issue multica-ai/multica#3711 — on self-hosted
// backends that pre-date the per-issue metadata route, agent runtime
// bootstrap calls `multica issue metadata list <issue> --output json`
// best-effort and any non-zero exit was being escalated by the Hermes
// provider into a failed agent run. The fix is to treat a 404 from
// /api/issues/{id}/metadata as "this server has no metadata yet" and
// emit `{}` with exit 0 — but only for `list`, since get/set/delete on
// a missing endpoint really are operational failures the caller asked
// for.

const testIssueUUID = "11111111-1111-1111-1111-111111111111"

func newIssueMetadataListTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "list"}
	c.Flags().String("output", "json", "")
	return c
}

func newIssueMetadataGetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "get"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

func newIssueMetadataSetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "set"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	c.Flags().String("value", "", "")
	c.Flags().String("type", "", "")
	return c
}

func newIssueMetadataDeleteTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "delete"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

// captureStdout used in this file is the (string, error) helper defined
// in cmd_skill_test.go.

// metadataTestServer wires a minimal fake backend that answers the
// resolveIssueRef GET on /api/issues/<id> and forwards every metadata
// request to the supplied handler. It returns the captured request paths
// in order so callers can assert routing.
func metadataTestServer(t *testing.T, metadataHandler http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testIssueUUID,
				"identifier": "MUL-1",
				"title":      "test issue",
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"+testIssueUUID+"/metadata"):
			metadataHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return srv, &paths
}

func TestRunIssueMetadataListDegradesOn404JSON(t *testing.T) {
	var hits int32
	_, paths := metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList returned error on 404, want nil: %v", runErr)
	}
	if got := strings.TrimSpace(out); got != "{}" {
		t.Fatalf("stdout = %q, want %q (empty JSON object on 404 degradation)", got, "{}")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("metadata endpoint hits = %d, want 1; routing: %v", got, *paths)
	}
}

func TestRunIssueMetadataListDegradesOn404Table(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such route", http.StatusNotFound)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "table")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList returned error on 404 table mode: %v", runErr)
	}
	// Table mode prints headers even with zero rows; the important
	// invariant is just that exit is clean and the row table doesn't
	// surface a stack trace or error blob.
	if !strings.Contains(out, "KEY") {
		t.Fatalf("table output missing KEY header, got %q", out)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(out, "404") {
		t.Fatalf("table output unexpectedly leaked error text: %q", out)
	}
}

func TestRunIssueMetadataListSuccessReturnsServerMetadata(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"pr_url":          "https://example.com/pr/1",
				"pipeline_status": "waiting_review",
			},
		})
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList: %v", runErr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["pr_url"] != "https://example.com/pr/1" || got["pipeline_status"] != "waiting_review" {
		t.Fatalf("stdout = %#v, missing expected keys", got)
	}
}

// 5xx and other non-404 errors must keep real error semantics — we only
// want to mask "this server has no metadata endpoint", not "the server
// is broken".
func TestRunIssueMetadataListPropagatesNon404Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	// Drop stdout to keep test output clean even if the implementation
	// regresses and prints something.
	_, err := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataList returned nil on 500, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want it to mention status 500", err)
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error chain missing *cli.HTTPError: %v", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("HTTPError.StatusCode = %d, want 500", httpErr.StatusCode)
	}
}

func TestRunIssueMetadataListPropagates401Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataList returned nil on 401, want error")
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 *cli.HTTPError, got %v", err)
	}
}

// get/set/delete must NOT degrade on 404 — those calls represent real
// caller intent and the user needs to see the failure.
func TestRunIssueMetadataGetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataGetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataGet(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataGet returned nil on 404, want error")
	}
}

func TestRunIssueMetadataSetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// PUT /metadata/<key> on an old server — must surface the failure.
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataSetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("value", "https://example.com/pr/1")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataSet(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataSet returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "set metadata") {
		t.Fatalf("error = %v, want it wrapped with 'set metadata' prefix", err)
	}
}

func TestRunIssueMetadataDeleteReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataDeleteTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataDelete(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataDelete returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "delete metadata") {
		t.Fatalf("error = %v, want it wrapped with 'delete metadata' prefix", err)
	}
}

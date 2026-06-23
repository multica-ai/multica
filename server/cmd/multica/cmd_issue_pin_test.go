package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newIssuePinTestCmd builds a throwaway cobra.Command carrying the flags
// runIssuePin reads, mirroring the issuePinCmd definition.
func newIssuePinTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pin"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newIssuePinsTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pins"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func TestRunIssuePinPostsItemTypeIssue(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/issue-1" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
				"title":      "Pin me",
			})
			return
		}
		if r.URL.Path != "/api/pins" {
			http.NotFound(w, r)
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "pin-1",
			"item_type":   "issue",
			"item_id":     "issue-1",
			"position":    1,
			"created_at":  "2026-06-22T08:00:00Z",
			"workspace_id": "ws-1",
			"user_id":     "user-1",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePinTestCmd()
	if err := runIssuePin(cmd, []string{"issue-1"}); err != nil {
		t.Fatalf("runIssuePin: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/pins" {
		t.Errorf("path = %s, want /api/pins", gotPath)
	}
	if gotBody["item_type"] != "issue" || gotBody["item_id"] != "issue-1" {
		t.Errorf("body = %+v, want item_type=issue item_id=issue-1", gotBody)
	}
}

func TestRunIssuePinAlreadyPinnedIsIdempotent(t *testing.T) {
	var pinnedConflict bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/MUL-1" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
				"title":      "Already pinned",
			})
			return
		}
		if r.URL.Path == "/api/pins" && r.Method == http.MethodPost {
			pinnedConflict = true
			http.Error(w, "item already pinned", http.StatusConflict)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePinTestCmd()
	if err := runIssuePin(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssuePin on already-pinned issue should be idempotent, got: %v", err)
	}
	if !pinnedConflict {
		t.Fatal("expected POST /api/pins to be hit")
	}
}

func TestRunIssueUnpinDeletesIssuePin(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues/MUL-1" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
				"title":      "Unpin me",
			})
			return
		}
		if r.URL.Path == "/api/pins/issue/issue-1" {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePinTestCmd() // unpin reads no flags; any cmd works
	if err := runIssueUnpin(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueUnpin: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/pins/issue/issue-1" {
		t.Errorf("path = %s, want /api/pins/issue/issue-1", gotPath)
	}
}

func TestRunIssuePinsFiltersToIssuesAndEnrichesTable(t *testing.T) {
	pinsResp := []map[string]any{
		{
			"id":         "pin-1",
			"item_type":  "issue",
			"item_id":    "issue-1",
			"position":   1,
			"created_at": "2026-06-22T08:00:00Z",
		},
		{
			"id":         "pin-2",
			"item_type":  "project",
			"item_id":    "proj-1",
			"position":   2,
			"created_at": "2026-06-22T09:00:00Z",
		},
		{
			"id":         "pin-3",
			"item_type":  "issue",
			"item_id":    "issue-2",
			"position":   3,
			"created_at": "2026-06-22T10:00:00Z",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pins" {
			json.NewEncoder(w).Encode(pinsResp)
			return
		}
		switch r.URL.Path {
		case "/api/issues/issue-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
				"title":      "First pinned",
				"status":     "in_progress",
			})
		case "/api/issues/issue-2":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-2",
				"identifier": "MUL-2",
				"title":      "Second pinned",
				"status":     "todo",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePinsTestCmd()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssuePins(cmd, nil)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssuePins: %v", err)
	}
	text := string(out)

	// Issue pins are rendered; the project pin is filtered out.
	for _, want := range []string{"MUL-1", "First pinned", "MUL-2", "Second pinned"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "proj-1") {
		t.Errorf("project pin leaked into issue pins output:\n%s", text)
	}
}

func TestRunIssuePinsJSONOutputsRawRows(t *testing.T) {
	pinsResp := []map[string]any{
		{"id": "pin-1", "item_type": "issue", "item_id": "issue-1", "position": 1, "created_at": "2026-06-22T08:00:00Z"},
		{"id": "pin-2", "item_type": "project", "item_id": "proj-1", "position": 2, "created_at": "2026-06-22T09:00:00Z"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pins" {
			json.NewEncoder(w).Encode(pinsResp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePinsTestCmd()
	_ = cmd.Flags().Set("output", "json")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssuePins(cmd, nil)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssuePins: %v", err)
	}

	// JSON output should be the filtered (issue-only) raw pin list: the
	// project pin is dropped and no per-issue enrichment fetch happens.
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal JSON output: %v\n%s", err, string(out))
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 issue pin, got %d: %+v", len(got), got)
	}
	if got[0]["item_id"] != "issue-1" || got[0]["item_type"] != "issue" {
		t.Errorf("unexpected pin row: %+v", got[0])
	}
}

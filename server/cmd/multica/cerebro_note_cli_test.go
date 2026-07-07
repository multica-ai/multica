// Tests for the FIR-2821 note write-path CLI: comment add/reply/resolve/send
// and note↔issue reference add/list. Each test drives the run function against
// an httptest server and asserts the HTTP method, path, and JSON body the CLI
// produced — the same harness the squad CLI tests use.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func newNoteCommentAddTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("body", "", "")
	cmd.Flags().String("reply-to", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newNoteCommentResolveTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "resolve"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().Bool("reopen", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newNoteCommentSendTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "send"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().StringArray("comment", nil, "")
	cmd.Flags().String("destination-object", "", "")
	cmd.Flags().String("destination-ref-id", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newNoteReferenceAddTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("object", "", "")
	cmd.Flags().String("ref-id", "", "")
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func withNoteTestEnv(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
}

func TestNoteWriteCommandsAreRegistered(t *testing.T) {
	cases := []struct {
		args  []string
		name  string
		flags []string
	}{
		{[]string{"comment", "add", "note-1"}, "add", []string{"body", "reply-to", "output"}},
		{[]string{"comment", "resolve", "note-1", "c-1"}, "resolve", []string{"reopen", "output"}},
		{[]string{"comment", "send", "note-1"}, "send", []string{"comment", "destination-object", "destination-ref-id"}},
		{[]string{"comment", "list", "note-1"}, "list", []string{"output"}},
		{[]string{"reference", "add", "note-1"}, "add", []string{"issue", "object", "ref-id", "type", "label", "url"}},
		{[]string{"reference", "list", "note-1"}, "list", []string{"output"}},
	}
	for _, tc := range cases {
		cmd, _, err := noteCmd.Find(tc.args)
		if err != nil {
			t.Fatalf("find %v: %v", tc.args, err)
		}
		if cmd == nil || cmd.Name() != tc.name {
			t.Fatalf("args %v resolved to %#v, want %q", tc.args, cmd, tc.name)
		}
		for _, f := range tc.flags {
			if cmd.Flags().Lookup(f) == nil {
				t.Errorf("command %q missing --%s flag", tc.name, f)
			}
		}
	}
}

func TestRunNoteCommentAddPostsBody(t *testing.T) {
	var gotMethod, gotPath, gotWS string
	var gotBody map[string]any
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotWS = r.Method, r.URL.Path, r.Header.Get("X-Workspace-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "body": "hello"})
	})

	cmd := newNoteCommentAddTestCmd()
	_ = cmd.Flags().Set("body", "hello")
	_ = cmd.Flags().Set("reply-to", "root-9")
	if err := runNoteCommentAdd(cmd, []string{"note-1"}); err != nil {
		t.Fatalf("runNoteCommentAdd: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/notes/note-1/comments" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotWS != "workspace-123" {
		t.Fatalf("X-Workspace-ID = %q", gotWS)
	}
	if gotBody["body"] != "hello" {
		t.Fatalf("body.body = %v", gotBody["body"])
	}
	if gotBody["thread_root_id"] != "root-9" {
		t.Fatalf("body.thread_root_id = %v, want root-9", gotBody["thread_root_id"])
	}
}

func TestRunNoteCommentAddRequiresBody(t *testing.T) {
	cmd := newNoteCommentAddTestCmd()
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	if err := runNoteCommentAdd(cmd, []string{"note-1"}); err == nil {
		t.Fatal("expected error when --body is empty")
	}
}

func TestRunNoteCommentResolveSendsResolvedFlag(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reopen     bool
		wantResolv bool
	}{
		{"resolve", false, true},
		{"reopen", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]any
			withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "c-1", "resolved": tc.wantResolv})
			})
			cmd := newNoteCommentResolveTestCmd()
			if tc.reopen {
				_ = cmd.Flags().Set("reopen", "true")
			}
			if err := runNoteCommentResolve(cmd, []string{"note-1", "c-1"}); err != nil {
				t.Fatalf("runNoteCommentResolve: %v", err)
			}
			if gotPath != "/api/notes/note-1/comments/c-1/resolve" {
				t.Fatalf("path = %q", gotPath)
			}
			if gotBody["resolved"] != tc.wantResolv {
				t.Fatalf("body.resolved = %v, want %v", gotBody["resolved"], tc.wantResolv)
			}
		})
	}
}

func TestRunNoteCommentSendPostsCommentIDs(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sent": []any{}, "unsent_remaining": 0, "destination_kind": "issue",
			"destination_ref_id": "issue-1", "agents_triggered": 1,
		})
	})
	cmd := newNoteCommentSendTestCmd()
	_ = cmd.Flags().Set("comment", "c-1")
	_ = cmd.Flags().Set("comment", "c-2")
	if err := runNoteCommentSend(cmd, []string{"note-1"}); err != nil {
		t.Fatalf("runNoteCommentSend: %v", err)
	}
	if gotPath != "/api/notes/note-1/comments/send" {
		t.Fatalf("path = %q", gotPath)
	}
	ids, ok := gotBody["comment_ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "c-1" || ids[1] != "c-2" {
		t.Fatalf("comment_ids = %v", gotBody["comment_ids"])
	}
}

func TestRunNoteReferenceAddWithObjectAndRefID(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ref-1", "object": "issue", "ref_id": "issue-uuid"})
	})
	cmd := newNoteReferenceAddTestCmd()
	_ = cmd.Flags().Set("object", "issue")
	_ = cmd.Flags().Set("ref-id", "issue-uuid")
	_ = cmd.Flags().Set("label", "context")
	if err := runNoteReferenceAdd(cmd, []string{"note-1"}); err != nil {
		t.Fatalf("runNoteReferenceAdd: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/notes/note-1/references" {
		t.Fatalf("method=%s path=%q", gotMethod, gotPath)
	}
	if gotBody["object"] != "issue" || gotBody["ref_id"] != "issue-uuid" || gotBody["label"] != "context" {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestRunNoteReferenceAddResolvesIssueFlag(t *testing.T) {
	const issueUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var refBody map[string]any
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/FIR-2821":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": issueUUID, "identifier": "FIR-2821", "title": "T"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/notes/note-1/references":
			_ = json.NewDecoder(r.Body).Decode(&refBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ref-1", "object": "issue", "ref_id": issueUUID})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cmd := newNoteReferenceAddTestCmd()
	_ = cmd.Flags().Set("issue", "FIR-2821")
	if err := runNoteReferenceAdd(cmd, []string{"note-1"}); err != nil {
		t.Fatalf("runNoteReferenceAdd: %v", err)
	}
	if refBody["object"] != "issue" || refBody["ref_id"] != issueUUID {
		t.Fatalf("reference body = %#v, want object=issue ref_id=%s", refBody, issueUUID)
	}
}

func TestRunNoteReferenceAddRejectsIssueWithObject(t *testing.T) {
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected; got %s %s", r.Method, r.URL.Path)
	})
	cmd := newNoteReferenceAddTestCmd()
	_ = cmd.Flags().Set("issue", "FIR-1")
	_ = cmd.Flags().Set("object", "issue")
	if err := runNoteReferenceAdd(cmd, []string{"note-1"}); err == nil {
		t.Fatal("expected error combining --issue with --object")
	}
}

func TestRunNoteReferenceAddRequiresTarget(t *testing.T) {
	withNoteTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected; got %s %s", r.Method, r.URL.Path)
	})
	cmd := newNoteReferenceAddTestCmd()
	if err := runNoteReferenceAdd(cmd, []string{"note-1"}); err == nil {
		t.Fatal("expected error when neither --issue nor --object/--ref-id given")
	}
}

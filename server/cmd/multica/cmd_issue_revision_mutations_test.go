package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetIssueRevisionCommand(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		value, changed := f.Value.String(), f.Changed
		t.Cleanup(func() { _ = f.Value.Set(value); f.Changed = changed })
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatal(err)
		}
		f.Changed = false
	})
}

func TestIssueConditionalUpdateRejectsSeparateMutations(t *testing.T) {
	for _, kind := range []string{"attachment", "pr-auto-complete"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("fixture.txt", []byte("synthetic file"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := newIssueUpdateTestCmd()
			if cmd.Flags().Lookup("pr-auto-complete") == nil {
				cmd.Flags().String("pr-auto-complete", "", "")
			}
			for key, value := range map[string]string{"title": "after", expectedRevisionFlag: "7"} {
				if err := cmd.Flags().Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			value := "off"
			if kind == "attachment" {
				value = "fixture.txt"
			}
			if err := cmd.Flags().Set(kind, value); err != nil {
				t.Fatal(err)
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if strings.HasSuffix(r.URL.Path, "/pr-auto-complete") {
					http.Error(w, "policy write failed", 500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": testIssueUUID, "identifier": "MUL-1", "description": "before", "url": "/fixture.txt", "revision": 8})
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			_, err := captureStdout(t, func() error { return runIssueUpdate(cmd, []string{testIssueUUID}) })
			if err == nil || !strings.Contains(err.Error(), "--expected-revision") {
				t.Fatalf("unsafe combination error=%v", err)
			}
			if requests != 0 {
				t.Fatalf("conditional update made %d requests before rejecting a separate mutation", requests)
			}
		})
	}
}

func TestIssueUpdateRevisionConflictJSON(t *testing.T) {
	cmd := newIssueUpdateTestCmd()
	for key, value := range map[string]string{"title": "after", expectedRevisionFlag: "7", "output": "json"} {
		if err := cmd.Flags().Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "revision_conflict", "error": "issue changed", "resource_type": "issue", "resource_id": testIssueUUID, "expected_revision": 7, "actual_revision": 8})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	out, err := captureStdout(t, func() error { return runIssueUpdate(cmd, []string{testIssueUUID}) })
	if err == nil || cli.ExitCodeFor(err) == 0 {
		t.Fatal("stale write must fail")
	}
	var result struct {
		Code             string `json:"code"`
		ExpectedRevision int64  `json:"expected_revision"`
		ActualRevision   int64  `json:"actual_revision"`
	}
	if decodeErr := json.Unmarshal([]byte(out), &result); decodeErr != nil {
		t.Fatalf("conflict stdout is not JSON: %q (%v)", out, decodeErr)
	}
	if result.Code != "revision_conflict" || result.ExpectedRevision != 7 || result.ActualRevision != 8 {
		t.Fatalf("conflict=%+v", result)
	}
	if requests != 1 {
		t.Fatalf("requests=%d; stale writes must not retry", requests)
	}
}

func TestIssueReorderRevisionPrecondition(t *testing.T) {
	const otherA = "44444444-4444-4444-4444-444444444444"
	const otherB = "55555555-5555-5555-5555-555555555555"
	for _, tc := range []struct {
		name, flag, value string
		single            bool
		wantPosition      float64
		wantRevision      int64
	}{
		{"move", "top", "true", false, -1, 8},
		{"already positioned", "before", otherB, false, 5, 7},
		{"single item", "top", "true", true, 5, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := mkIssue(testIssueUUID, "MUL-1", "todo", 5)
			target["revision"] = 7
			column := []any{mkIssue(otherA, "MUL-2", "todo", 0), target, mkIssue(otherB, "MUL-3", "todo", 10)}
			if tc.single {
				column = []any{target}
			}
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/issues" {
					writeReorderColumnPage(w, r, column)
					return
				}
				if r.URL.Path == "/api/issues/"+testIssueUUID || r.URL.Path == "/api/issues/"+testIssueUUID+"/conditional" {
					if r.Method == http.MethodPut {
						if r.URL.Path != "/api/issues/"+testIssueUUID+"/conditional" {
							t.Errorf("unprotected reorder path: %s", r.URL.Path)
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						target["revision"] = tc.wantRevision
						target["position"] = body["position"]
					}
					_ = json.NewEncoder(w).Encode(target)
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			cmd := newIssueReorderTestCmd()
			_ = cmd.Flags().Set(tc.flag, tc.value)
			if err := cmd.Flags().Set(expectedRevisionFlag, "7"); err != nil {
				t.Fatal(err)
			}
			out, err := captureStdout(t, func() error { return runIssueReorder(cmd, []string{testIssueUUID}) })
			if err != nil {
				t.Fatal(err)
			}
			if body["expected_revision"] != float64(7) || body["position"] != tc.wantPosition {
				t.Fatalf("conditional reorder body=%v", body)
			}
			var got struct {
				Revision int64 `json:"revision"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if got.Revision != tc.wantRevision {
				t.Fatalf("returned revision=%d, want %d", got.Revision, tc.wantRevision)
			}
		})
	}
}

func TestIssueMetadataConditionalCLI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cmd    *cobra.Command
		method string
		flags  map[string]string
	}{
		{"set", issueMetadataSetCmd, http.MethodPut, map[string]string{"key": "state", "value": "after"}},
		{"delete", issueMetadataDeleteCmd, http.MethodDelete, map[string]string{"key": "state"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetIssueRevisionCommand(t, tc.cmd)
			for name, value := range tc.flags {
				if err := tc.cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := tc.cmd.Flags().Set(expectedRevisionFlag, "7"); err != nil {
				t.Fatal(err)
			}
			_ = tc.cmd.Flags().Set("output", "json")
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != tc.method || r.URL.Path != "/api/issues/"+testIssueUUID+"/conditional/metadata/state" || r.URL.Query().Get("expected_revision") != "7" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"state": "after"}, "issue_revision": 8})
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			out, err := captureStdout(t, func() error { return tc.cmd.RunE(tc.cmd, []string{testIssueUUID}) })
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				IssueRevision int64 `json:"issue_revision"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if got.IssueRevision != 8 {
				t.Fatalf("missing authoritative mutation revision: %s", out)
			}
			if requests != 1 {
				t.Fatalf("requests=%d, want one guarded mutation without a follow-up read", requests)
			}
		})
	}
}

func TestIssueConditionalMetadataDoesNotFallBackOnOldServer(t *testing.T) {
	cmd := issueMetadataSetCmd
	resetIssueRevisionCommand(t, cmd)
	for key, value := range map[string]string{"key": "state", "value": "after", expectedRevisionFlag: "7"} {
		if err := cmd.Flags().Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/conditional/") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"state": "unconditionally overwritten"}, "issue_revision": 8})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	err := cmd.RunE(cmd, []string{testIssueUUID})
	if err == nil {
		t.Fatal("conditional write succeeded on an old server")
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "/conditional/") {
		t.Fatalf("unsafe fallback requests: %v", paths)
	}
}

func TestIssuePropertyAndLabelConditionalCLI(t *testing.T) {
	const relatedID = "33333333-3333-3333-3333-333333333333"
	for _, tc := range []struct {
		name           string
		cmd            *cobra.Command
		method, suffix string
		flags          map[string]string
		args           []string
		property       bool
	}{
		{"property set", issuePropertySetCmd, http.MethodPut, "/properties/" + relatedID, map[string]string{"name": relatedID, "value": "after"}, []string{testIssueUUID}, true},
		{"property unset", issuePropertyUnsetCmd, http.MethodDelete, "/properties/" + relatedID, map[string]string{"name": relatedID}, []string{testIssueUUID}, true},
		{"label add", issueLabelAddCmd, http.MethodPost, "/labels", nil, []string{testIssueUUID, relatedID}, false},
		{"label remove", issueLabelRemoveCmd, http.MethodDelete, "/labels/" + relatedID, nil, []string{testIssueUUID, relatedID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetIssueRevisionCommand(t, tc.cmd)
			for key, value := range tc.flags {
				if err := tc.cmd.Flags().Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := tc.cmd.Flags().Set(expectedRevisionFlag, "7"); err != nil {
				t.Fatal(err)
			}
			_ = tc.cmd.Flags().Set("output", "json")
			writes, reads := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.property && r.Method == http.MethodGet && r.URL.Path == "/api/properties" {
					reads++
					_ = json.NewEncoder(w).Encode(map[string]any{"properties": []map[string]any{{"id": relatedID, "name": "State", "type": "text", "config": map[string]any{}}}})
					return
				}
				writes++
				if r.Method != tc.method || r.URL.Path != "/api/issues/"+testIssueUUID+"/conditional"+tc.suffix || r.URL.Query().Get("expected_revision") != "7" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"properties": map[string]any{relatedID: "after"}, "labels": []map[string]any{{"id": relatedID, "name": "Bug", "color": "#aabbcc"}}, "issue_revision": 8})
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			out, err := captureStdout(t, func() error { return tc.cmd.RunE(tc.cmd, tc.args) })
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				IssueRevision int64 `json:"issue_revision"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result.IssueRevision != 8 {
				t.Fatalf("missing authoritative revision: %s", out)
			}
			wantReads := 0
			if tc.property {
				wantReads = 1
			}
			if writes != 1 || reads != wantReads {
				t.Fatalf("writes=%d reads=%d, want 1 and %d", writes, reads, wantReads)
			}
		})
	}
}

func TestPrimaryConditionalWritesFailClosedOnOldServer(t *testing.T) {
	for _, name := range []string{"update", "status", "assign", "reorder"} {
		t.Run(name, func(t *testing.T) {
			var cmd *cobra.Command
			args := []string{testIssueUUID}
			var run func(*cobra.Command, []string) error
			switch name {
			case "update":
				cmd = newIssueUpdateTestCmd()
				_ = cmd.Flags().Set("title", "after")
				run = runIssueUpdate
			case "status":
				cmd = newIssueStatusTestCmd()
				args = append(args, "todo")
				run = runIssueStatus
			case "assign":
				cmd = newIssueAssignTestCmd()
				_ = cmd.Flags().Set("unassign", "true")
				run = runIssueAssign
			case "reorder":
				cmd = newIssueReorderTestCmd()
				_ = cmd.Flags().Set("top", "true")
				run = runIssueReorder
			}
			_ = cmd.Flags().Set(expectedRevisionFlag, "7")
			mutations, protected := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					if r.URL.Path == "/api/issues/"+testIssueUUID+"/conditional" {
						protected++
						http.NotFound(w, r)
						return
					}
					mutations++
				}
				target := mkIssue(testIssueUUID, "MUL-1", "todo", 5)
				if r.URL.Path == "/api/issues" {
					writeReorderColumnPage(w, r, []any{target})
					return
				}
				_ = json.NewEncoder(w).Encode(target)
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			_, err := captureStdout(t, func() error { return run(cmd, args) })
			if err == nil || mutations != 0 || protected != 1 {
				t.Fatalf("err=%v unconditional=%d protected=%d", err, mutations, protected)
			}
		})
	}
}

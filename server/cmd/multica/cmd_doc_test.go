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
	expected := map[string]bool{
		"list": true, "search": true, "get": true, "create": true,
		"edit": true, "update": true, "move": true, "folder": true,
	}
	for _, command := range docCmd.Commands() {
		if !expected[command.Name()] {
			t.Fatalf("unexpected doc command: %s", command.Name())
		}
		delete(expected, command.Name())
	}
	if len(expected) != 0 {
		t.Fatalf("missing doc commands: %v", expected)
	}

	expectedFolder := map[string]bool{"list": true, "create": true, "update": true, "move": true}
	for _, command := range docFolderCmd.Commands() {
		if !expectedFolder[command.Name()] {
			t.Fatalf("unexpected doc folder command: %s", command.Name())
		}
		delete(expectedFolder, command.Name())
	}
	if len(expectedFolder) != 0 {
		t.Fatalf("missing doc folder commands: %v", expectedFolder)
	}
	if !docFolderUpdateCmd.HasAlias("rename") {
		t.Fatal("doc folder update should accept rename as an alias")
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

func TestRunDocMoveSendsDestination(t *testing.T) {
	const docID = "11111111-2222-3333-4444-555555555555"
	const folderID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": docID, "title": "Runbook", "folder_id": gotBody["folder_id"]})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	for _, tc := range []struct {
		folder string
		want   any
	}{{folderID, folderID}, {"root", nil}} {
		_ = docMoveCmd.Flags().Set("folder", tc.folder)
		if _, err := captureStdout(t, func() error { return runDocMove(docMoveCmd, []string{docID}) }); err != nil {
			t.Fatalf("runDocMove(%s): %v", tc.folder, err)
		}
		if gotPath != "/api/docs/"+docID+"/move" || gotBody["folder_id"] != tc.want {
			t.Fatalf("--folder %s sent %s %v", tc.folder, gotPath, gotBody)
		}
	}
	_ = docMoveCmd.Flags().Set("folder", "")
	if err := runDocMove(docMoveCmd, []string{docID}); err == nil || !strings.Contains(err.Error(), "--folder is required") {
		t.Fatalf("missing --folder must explain itself: %v", err)
	}
}

func TestRunDocFolderListFormatsTableAndJSON(t *testing.T) {
	const rootID = "11111111-2222-3333-4444-555555555555"
	const childID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var returnNilFolders bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/docs" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if returnNilFolders {
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"folders": []any{
				map[string]any{"id": childID, "title": "Release", "parent_id": rootID, "updated_at": "2026-11-22T01:02:03Z"},
				map[string]any{"id": rootID, "title": "Guides", "parent_id": nil, "updated_at": "2026-11-21T01:02:03Z"},
			},
			"docs": []any{},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Cleanup(func() { _ = docFolderListCmd.Flags().Set("output", "table") })

	_ = docFolderListCmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runDocFolderList(docFolderListCmd, nil) })
	if err != nil {
		t.Fatalf("runDocFolderList table: %v", err)
	}
	if !strings.Contains(out, "Guides/") || !strings.Contains(out, "Guides/Release/") {
		t.Fatalf("table output missing folder paths: %q", out)
	}
	if strings.Index(out, rootID) > strings.Index(out, childID) {
		t.Fatalf("table output should sort parent before child: %q", out)
	}

	_ = docFolderListCmd.Flags().Set("output", "json")
	out, err = captureStdout(t, func() error { return runDocFolderList(docFolderListCmd, nil) })
	if err != nil {
		t.Fatalf("runDocFolderList json: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 {
		t.Fatalf("json output = %q (err=%v)", out, err)
	}

	returnNilFolders = true
	out, err = captureStdout(t, func() error { return runDocFolderList(docFolderListCmd, nil) })
	if err != nil {
		t.Fatalf("runDocFolderList empty json: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty json output = %q, want []", out)
	}
}

func TestRunDocFolderCreateSendsTitleAndParent(t *testing.T) {
	const parentID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	const createdID = "11111111-2222-3333-4444-555555555555"
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/docs" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"folders": []any{map[string]any{"id": parentID, "title": "Guides", "parent_id": nil}},
				"docs":    []any{},
			})
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        createdID,
			"title":     gotBody["title"],
			"parent_id": gotBody["parent_id"],
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Cleanup(func() {
		_ = docFolderCreateCmd.Flags().Set("title", "")
		_ = docFolderCreateCmd.Flags().Set("parent", "")
		_ = docFolderCreateCmd.Flags().Set("output", "json")
	})

	_ = docFolderCreateCmd.Flags().Set("title", "")
	if err := runDocFolderCreate(docFolderCreateCmd, nil); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("missing --title must fail: %v", err)
	}

	// Root folder (no --parent)
	_ = docFolderCreateCmd.Flags().Set("title", "Guides")
	_ = docFolderCreateCmd.Flags().Set("parent", "")
	_ = docFolderCreateCmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runDocFolderCreate(docFolderCreateCmd, nil) })
	if err != nil {
		t.Fatalf("runDocFolderCreate root: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/doc-folders" || gotBody["title"] != "Guides" {
		t.Fatalf("root create sent %s %s %v", gotMethod, gotPath, gotBody)
	}
	if _, hasParent := gotBody["parent_id"]; hasParent {
		t.Fatalf("root create should omit parent_id: %v", gotBody)
	}
	if !strings.Contains(out, createdID) || !strings.Contains(out, "root") {
		t.Fatalf("table output missing summary: %q", out)
	}

	// Nested folder via unique ID prefix, plus explicit --parent root
	_ = docFolderCreateCmd.Flags().Set("output", "json")
	for _, tc := range []struct {
		parent string
		want   any
	}{
		{parentID, parentID},
		{"aaaaaaaa", parentID},
		{"root", nil},
	} {
		_ = docFolderCreateCmd.Flags().Set("title", "Runbooks")
		_ = docFolderCreateCmd.Flags().Set("parent", tc.parent)
		if _, err := captureStdout(t, func() error { return runDocFolderCreate(docFolderCreateCmd, nil) }); err != nil {
			t.Fatalf("runDocFolderCreate(--parent %s): %v", tc.parent, err)
		}
		if gotMethod != http.MethodPost || gotPath != "/api/doc-folders" || gotBody["parent_id"] != tc.want {
			t.Fatalf("--parent %s sent %s %s %v", tc.parent, gotMethod, gotPath, gotBody)
		}
	}
}

func TestRunDocFolderUpdateRenamesFolder(t *testing.T) {
	const folderID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/docs" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"folders": []any{map[string]any{"id": folderID, "title": "Guides", "parent_id": nil}},
				"docs":    []any{},
			})
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": folderID, "title": gotBody["title"], "parent_id": nil})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Cleanup(func() {
		_ = docFolderUpdateCmd.Flags().Set("title", "")
		_ = docFolderUpdateCmd.Flags().Set("output", "json")
	})

	_ = docFolderUpdateCmd.Flags().Set("title", "")
	if err := runDocFolderUpdate(docFolderUpdateCmd, []string{folderID}); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("missing --title must fail: %v", err)
	}

	_ = docFolderUpdateCmd.Flags().Set("title", "Engineering Guides")
	if _, err := captureStdout(t, func() error { return runDocFolderUpdate(docFolderUpdateCmd, []string{"aaaaaaaa"}) }); err != nil {
		t.Fatalf("runDocFolderUpdate prefix: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/doc-folders/"+folderID || gotBody["title"] != "Engineering Guides" {
		t.Fatalf("update sent %s %s %v", gotMethod, gotPath, gotBody)
	}
}

func TestRunDocFolderMoveSendsParent(t *testing.T) {
	const folderID = "11111111-2222-3333-4444-555555555555"
	const parentID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/docs" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"folders": []any{
					map[string]any{"id": folderID, "title": "Child", "parent_id": nil},
					map[string]any{"id": parentID, "title": "Parent", "parent_id": nil},
				},
				"docs": []any{},
			})
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": folderID, "title": "Child", "parent_id": gotBody["parent_id"]})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Cleanup(func() {
		_ = docFolderMoveCmd.Flags().Set("parent", "")
		_ = docFolderMoveCmd.Flags().Set("output", "json")
	})

	for _, tc := range []struct {
		ref    string
		parent string
		want   any
	}{
		{folderID, parentID, parentID},
		{"11111111", "aaaaaaaa", parentID},
		{folderID, "root", nil},
	} {
		_ = docFolderMoveCmd.Flags().Set("parent", tc.parent)
		if _, err := captureStdout(t, func() error { return runDocFolderMove(docFolderMoveCmd, []string{tc.ref}) }); err != nil {
			t.Fatalf("runDocFolderMove(%s, %s): %v", tc.ref, tc.parent, err)
		}
		if gotMethod != http.MethodPost || gotPath != "/api/doc-folders/"+folderID+"/move" || gotBody["parent_id"] != tc.want {
			t.Fatalf("move(%s, --parent %s) sent %s %s %v", tc.ref, tc.parent, gotMethod, gotPath, gotBody)
		}
	}

	_ = docFolderMoveCmd.Flags().Set("parent", "")
	if err := runDocFolderMove(docFolderMoveCmd, []string{folderID}); err == nil || !strings.Contains(err.Error(), "--parent is required") {
		t.Fatalf("missing --parent must explain itself: %v", err)
	}
}

func TestRunDocFolderUpdateRejectsEmptyAndNonUUIDRefs(t *testing.T) {
	const folderID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/docs" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"folders": []any{map[string]any{"id": folderID, "title": "Guides", "parent_id": nil}},
				"docs":    []any{},
			})
			return
		}
		writes++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": folderID, "title": "X"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Cleanup(func() { _ = docFolderUpdateCmd.Flags().Set("title", "") })

	_ = docFolderUpdateCmd.Flags().Set("title", "X")
	for _, ref := range []string{"", "   "} {
		if err := runDocFolderUpdate(docFolderUpdateCmd, []string{ref}); err == nil || !strings.Contains(err.Error(), "folder id is required") {
			t.Fatalf("ref %q must be rejected: %v", ref, err)
		}
	}
	nonUUID := "aaaaaaaa/../../../docs?x=1#yyyyyyyyyy"[:36]
	if err := runDocFolderUpdate(docFolderUpdateCmd, []string{nonUUID}); err == nil || !strings.Contains(err.Error(), "no folder matches") {
		t.Fatalf("36-char non-UUID ref must not be used as an id: %v", err)
	}
	if writes != 0 {
		t.Fatalf("rejected refs must not reach a write endpoint, got %d writes", writes)
	}
}

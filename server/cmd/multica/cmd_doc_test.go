package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestDocCommandsExposeOnlyAgentReadWrite(t *testing.T) {
	expected := map[string]bool{
		"list": true, "search": true, "get": true, "create": true,
		"edit": true, "update": true, "move": true, "history": true,
		"restore": true, "comment": true, "folder": true,
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
	// Agents reply in doc discussions; resolving and deleting stay with people.
	if names := commandNames(docCommentCmd); len(names) != 2 || names[0] != "add" || names[1] != "list" {
		t.Fatalf("doc comment commands = %v, want add and list", names)
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

func TestDocMergeConflictErrorForbidsResendingTheBody(t *testing.T) {
	err := docWriteError("update doc", 3, &cli.HTTPError{StatusCode: http.StatusConflict, Body: `{"code":"doc_merge_conflict"}`})
	for _, want := range []string{"nothing was written", "redo your change", "Do not resend the same body"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("merge conflict error missing %q: %v", want, err)
		}
	}
}

// renderHelp renders cmd's --help the way the CLI prints it. Group commands
// use subHelpTemplate, which prints only Short, so rules agents need must
// live on the leaf commands the brief points at.
func renderHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	var out strings.Builder
	cmd.SetOut(&out)
	defer cmd.SetOut(nil)
	if err := cmd.Help(); err != nil {
		t.Fatalf("render %s help: %v", cmd.CommandPath(), err)
	}
	return out.String()
}

func TestDocHelpRendersWriteRules(t *testing.T) {
	tests := []struct {
		cmd  *cobra.Command
		want []string
	}{
		{docUpdateCmd, []string{"--base-revision", "refused, with nothing written", "never resend the same body"}},
		{docEditCmd, []string{"without --replace-all, appears more than once", "this or --old-file is required", "this or --new-file is required"}},
		{docCommentListCmd, []string{"reserved for people", "multica doc comment add"}},
		{docCommentAddCmd, []string{"--parent string", "reserved for people"}},
		{docMoveCmd, []string{`--folder string`, `or "root" for the top level`}},
		{docFolderCreateCmd, []string{`--parent string`, `or "root" for the top level`}},
		{docFolderMoveCmd, []string{`--parent string`, `or "root" for the top level`}},
	}
	for _, tt := range tests {
		t.Run(tt.cmd.CommandPath(), func(t *testing.T) {
			help := renderHelp(t, tt.cmd)
			for _, want := range tt.want {
				if !strings.Contains(help, want) {
					t.Fatalf("%s --help missing %q; rendered:\n%s", tt.cmd.CommandPath(), want, help)
				}
			}
		})
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
	if err := runDocFolderUpdate(docFolderUpdateCmd, []string{nonUUID}); err == nil || !strings.Contains(err.Error(), "resolve folder") {
		t.Fatalf("36-char non-UUID ref must not be used as an id: %v", err)
	}
	if writes != 0 {
		t.Fatalf("rejected refs must not reach a write endpoint, got %d writes", writes)
	}
}

func TestResolveDocRefMatchesSharedPrefixRules(t *testing.T) {
	const docA = "abcd1234-0000-0000-0000-000000000001"
	const docB = "abcd5678-0000-0000-0000-000000000002"
	const folderID = "fedc1234-0000-0000-0000-000000000003"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"folders": []any{map[string]any{"id": folderID, "title": "Guides"}},
			"docs": []any{
				map[string]any{"id": docA, "title": "Runbook"},
				map[string]any{"id": docB, "title": "Roadmap"},
			},
		})
	}))
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "ws-1", "mat_test-token")
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		ref     string
		folders bool
		want    string
		wantErr []string
	}{
		{name: "uppercase prefix", ref: "ABCD1", want: docA},
		{name: "prefix with dash", ref: "abcd5678-0", want: docB},
		{name: "folder prefix", ref: "FEDC", folders: true, want: folderID},
		{name: "doc prefix does not match folders", ref: "abcd1", folders: true, wantErr: []string{"no folder found", "`multica doc folder list`"}},
		{name: "unmatched doc prefix", ref: "0000", wantErr: []string{"no doc found", "`multica doc list`"}},
		{name: "too short prefix", ref: "abc", wantErr: []string{"resolve doc", "at least 4"}},
		{name: "ambiguous prefix lists candidates", ref: "abcd", wantErr: []string{"ambiguous doc id prefix", docA, docB, "`multica doc list`"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDocRef(ctx, client, tc.ref, tc.folders)
			if tc.wantErr == nil {
				if err != nil || got != tc.want {
					t.Fatalf("resolveDocRef(%q) = %q, %v; want %q", tc.ref, got, err, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveDocRef(%q) = %q; want error", tc.ref, got)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("resolveDocRef(%q) error = %v; want it to contain %q", tc.ref, err, want)
				}
			}
			if strings.Contains(err.Error(), "--full-id") {
				t.Fatalf("resolveDocRef(%q) error = %v; doc list commands have no --full-id flag", tc.ref, err)
			}
		})
	}
}

func commandNames(cmd interface{ Commands() []*cobra.Command }) []string {
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	return names
}

func setDocTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
}

// setDocFlag sets a flag on a shared command and restores its default after the test.
func setDocFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	f := cmd.Flags().Lookup(name)
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Flags().Set(name, f.DefValue)
		f.Changed = false
	})
}

func TestRunDocHistoryListsVersionsAndShowsOne(t *testing.T) {
	const docID = "11111111-2222-3333-4444-555555555555"
	var gotPaths []string
	setDocTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/docs/" + docID + "/versions":
			_ = json.NewEncoder(w).Encode(map[string]any{"versions": []any{
				map[string]any{"number": 3, "action": "restore", "action_ref": 1, "author_type": "agent",
					"author_id": "aaaaaaaa-0000-0000-0000-000000000000", "message": "roll back", "updated_at": "2026-11-23T04:53:02Z"},
				map[string]any{"number": 2, "action": "edit", "author_type": "member",
					"author_id": "bbbbbbbb-0000-0000-0000-000000000000", "message": "", "updated_at": "2026-11-22T10:00:00Z"},
			}})
		case "/api/docs/" + docID + "/versions/2":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 2, "title": "Runbook", "content": "# Old body\n"})
		default:
			http.NotFound(w, r)
		}
	})

	out, err := captureStdout(t, func() error { return runDocHistory(docHistoryCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"restore of v1", "agent:aaaaaaaa", "roll back", "2026-11-23 04:53", "member:bbbbbbbb"} {
		if !strings.Contains(out, want) {
			t.Fatalf("history table missing %q:\n%s", want, out)
		}
	}

	setDocFlag(t, docHistoryCmd, "version", "2")
	setDocFlag(t, docHistoryCmd, "output", "content")
	out, err = captureStdout(t, func() error { return runDocHistory(docHistoryCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("show version: %v", err)
	}
	if out != "# Old body\n" {
		t.Fatalf("--output content = %q", out)
	}
	if last := gotPaths[len(gotPaths)-1]; last != "/api/docs/"+docID+"/versions/2" {
		t.Fatalf("version request path = %s", last)
	}

	setDocFlag(t, docHistoryCmd, "output", "table")
	if err := runDocHistory(docHistoryCmd, []string{docID}); err == nil || !strings.Contains(err.Error(), "json or content") {
		t.Fatalf("table output for one version must be refused: %v", err)
	}
}

func TestRunDocRestoreSendsRestoreAction(t *testing.T) {
	const docID = "11111111-2222-3333-4444-555555555555"
	var gotMethod, gotPath string
	var gotBody map[string]any
	conflict := false
	setDocTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		if conflict {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "revision_conflict", "actual_revision": 9})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": docID, "title": "Runbook", "revision": 8, "content": "restored"})
	})

	if err := runDocRestore(docRestoreCmd, []string{docID}); err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("missing --version must explain itself: %v", err)
	}
	setDocFlag(t, docRestoreCmd, "version", "2")
	if err := runDocRestore(docRestoreCmd, []string{docID}); err == nil || !strings.Contains(err.Error(), "--base-revision is required") {
		t.Fatalf("missing --base-revision must explain itself: %v", err)
	}
	setDocFlag(t, docRestoreCmd, "base-revision", "7")
	setDocFlag(t, docRestoreCmd, "message", "undo broken rewrite")
	out, err := captureStdout(t, func() error { return runDocRestore(docRestoreCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/docs/"+docID {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["action"] != "restore" || gotBody["action_ref"] != float64(2) || gotBody["base_revision"] != float64(7) ||
		gotBody["message"] != "undo broken rewrite" {
		t.Fatalf("body = %v", gotBody)
	}
	if _, ok := gotBody["content"]; ok {
		t.Fatalf("restore must not send content: %v", gotBody)
	}
	if strings.Contains(out, "restored") {
		t.Fatalf("restore output should omit the doc body: %s", out)
	}

	conflict = true
	err = runDocRestore(docRestoreCmd, []string{docID})
	if err == nil || !strings.Contains(err.Error(), "nothing was restored") || !strings.Contains(err.Error(), "only if the whole doc should still go back") {
		t.Fatalf("stale revision must ask to re-read before restoring again: %v", err)
	}
	if strings.Contains(err.Error(), "merge your change") {
		t.Fatalf("a restore has no change to merge: %v", err)
	}
}

func TestRunDocCommentListGroupsThreadsAndHidesResolved(t *testing.T) {
	const docID = "11111111-2222-3333-4444-555555555555"
	var gotPath string
	setDocTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// Server order is chronological, so a reply can come after a later root.
		_ = json.NewEncoder(w).Encode(map[string]any{"comments": []any{
			map[string]any{"id": "c-open", "author_type": "member", "author_id": "m1", "content": "Cite the source\nhere",
				"anchor_quote": "growth doubled"},
			map[string]any{"id": "c-done", "author_type": "member", "author_id": "m1", "content": "typo",
				"anchor_quote": "teh", "resolved_at": "2026-11-22T00:00:00Z"},
			map[string]any{"id": "c-reply", "parent_id": "c-open", "author_type": "member", "author_id": "m2", "content": "agreed"},
			map[string]any{"id": "c-done-reply", "parent_id": "c-done", "author_type": "member", "author_id": "m2", "content": "fixed"},
		}})
	})

	setDocFlag(t, docCommentListCmd, "output", "json")
	out, err := captureStdout(t, func() error { return runDocCommentList(docCommentListCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if gotPath != "/api/docs/"+docID+"/comments" {
		t.Fatalf("request path = %s", gotPath)
	}
	var threads []map[string]any
	if err := json.Unmarshal([]byte(out), &threads); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if ids := commentIDs(threads); strings.Join(ids, ",") != "c-open,c-reply" {
		t.Fatalf("default list = %v, want the open thread with its reply", ids)
	}

	setDocFlag(t, docCommentListCmd, "include-resolved", "true")
	out, err = captureStdout(t, func() error { return runDocCommentList(docCommentListCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("list --include-resolved: %v", err)
	}
	threads = nil
	_ = json.Unmarshal([]byte(out), &threads)
	if ids := commentIDs(threads); strings.Join(ids, ",") != "c-open,c-reply,c-done,c-done-reply" {
		t.Fatalf("--include-resolved list = %v", ids)
	}

	setDocFlag(t, docCommentListCmd, "thread", "c-done")
	out, err = captureStdout(t, func() error { return runDocCommentList(docCommentListCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("list --thread: %v", err)
	}
	threads = nil
	_ = json.Unmarshal([]byte(out), &threads)
	if ids := commentIDs(threads); strings.Join(ids, ",") != "c-done,c-done-reply" {
		t.Fatalf("--thread list = %v, want only that discussion even when resolved", ids)
	}
	setDocFlag(t, docCommentListCmd, "thread", "c-missing")
	if _, err = captureStdout(t, func() error { return runDocCommentList(docCommentListCmd, []string{docID}) }); err == nil {
		t.Fatal("--thread with an unknown discussion should fail")
	}
	setDocFlag(t, docCommentListCmd, "thread", "")

	setDocFlag(t, docCommentListCmd, "output", "table")
	out, err = captureStdout(t, func() error { return runDocCommentList(docCommentListCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	for _, want := range []string{"growth doubled", "Cite the source here", "resolved", "open"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}

func TestRunDocCommentAddRepliesInDiscussion(t *testing.T) {
	const docID = "11111111-2222-3333-4444-555555555555"
	var gotPath string
	var gotBody map[string]any
	setDocTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c-new", "parent_id": "c-root", "content": "Done."})
	})

	setDocFlag(t, docCommentAddCmd, "content", "")
	setDocFlag(t, docCommentAddCmd, "parent", "")
	if _, err := captureStdout(t, func() error { return runDocCommentAdd(docCommentAddCmd, []string{docID}) }); err == nil ||
		!strings.Contains(err.Error(), "--parent") {
		t.Fatalf("a reply without --parent must fail, got %v", err)
	}

	setDocFlag(t, docCommentAddCmd, "parent", "c-root")
	setDocFlag(t, docCommentAddCmd, "content", "Done.")
	setDocFlag(t, docCommentAddCmd, "output", "json")
	out, err := captureStdout(t, func() error { return runDocCommentAdd(docCommentAddCmd, []string{docID}) })
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if gotPath != "/api/docs/"+docID+"/comments" || gotBody["parent_id"] != "c-root" || gotBody["content"] != "Done." {
		t.Fatalf("request = %s %v", gotPath, gotBody)
	}
	if !strings.Contains(out, "c-new") {
		t.Fatalf("output missing the new comment: %s", out)
	}
}

func commentIDs(comments []map[string]any) []string {
	ids := make([]string, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, strVal(c, "id"))
	}
	return ids
}

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// searchIndexWorkspace creates an isolated workspace so manifest and snapshot
// totals are not affected by rows other tests leave in the shared workspace.
func searchIndexWorkspace(t *testing.T, prefix string) string {
	t.Helper()
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	wsID := dbfx.Workspace(t, "Search index "+token, "search-index-"+token, testutil.Cols{"issue_prefix": prefix})
	// Registered before any fixture row, so it runs after their cleanups and
	// also removes the change rows those deletes log.
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM search_index_change WHERE workspace_id = $1`, wsID)
	})
	return wsID
}

func searchIndexRequest(method, path, wsID string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Workspace-ID", wsID)
	return req
}

func searchIndexManifest(t *testing.T, wsID string) SearchIndexManifestResponse {
	t.Helper()
	return testutil.Decode[SearchIndexManifestResponse](t, testHandler.GetSearchIndexManifest,
		searchIndexRequest(http.MethodGet, "/api/search-index/manifest", wsID, nil), http.StatusOK)
}

func searchIndexChanges(t *testing.T, wsID, cursor string, limit int) SearchIndexChangesResponse {
	t.Helper()
	return testutil.Decode[SearchIndexChangesResponse](t, testHandler.ListSearchIndexChanges,
		searchIndexRequest(http.MethodPost, "/api/search-index/changes", wsID, map[string]any{"cursor": cursor, "limit": limit}),
		http.StatusOK)
}

func issueIDs(issues []SearchIndexIssue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.ID)
	}
	sort.Strings(out)
	return out
}

func commentIDs(comments []SearchIndexComment) []string {
	out := make([]string, 0, len(comments))
	for _, c := range comments {
		out = append(out, c.ID)
	}
	sort.Strings(out)
	return out
}

func sortedIDs(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func assertIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(sortedIDs(want...)) {
		t.Fatalf("%s = %v, want %v", label, got, sortedIDs(want...))
	}
}

func TestSearchIndexBootstrapThenCatchUp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := searchIndexWorkspace(t, "SIX")
	first := dbfx.Issue(t, "First indexed issue", testutil.Cols{"workspace_id": wsID, "description": "alpha body"})
	second := dbfx.Issue(t, "Second indexed issue", testutil.Cols{"workspace_id": wsID, "description": nil})
	liveComment := dbfx.Comment(t, first, "first comment", testutil.Cols{"workspace_id": wsID})
	editedComment := dbfx.Comment(t, first, "comment to edit", testutil.Cols{"workspace_id": wsID})
	dbfx.Comment(t, first, "", testutil.Cols{"workspace_id": wsID, "deleted_at": time.Now()})
	project := dbfx.Project(t, "Indexed project", testutil.Cols{"workspace_id": wsID, "description": "project body"})

	manifest := searchIndexManifest(t, wsID)
	if manifest.IssueCount != 2 || manifest.CommentCount != 2 || manifest.ProjectCount != 1 {
		t.Fatalf("manifest counts = %+v, want 2 issues, 2 live comments, 1 project", manifest)
	}
	wantBytes := int64(len("First indexed issue") + len("alpha body") + len("Second indexed issue") +
		len("first comment") + len("comment to edit") + len("Indexed project") + len("project body"))
	if manifest.TextBytes != wantBytes {
		t.Fatalf("manifest text_bytes = %d, want %d", manifest.TextBytes, wantBytes)
	}

	page := testutil.Decode[SearchIndexSnapshotResponse](t, testHandler.GetSearchIndexSnapshot,
		searchIndexRequest(http.MethodGet, "/api/search-index/snapshot?limit=1", wsID, nil), http.StatusOK)
	assertIDs(t, "first page issues", issueIDs(page.Issues), []string{first})
	assertIDs(t, "first page comments", commentIDs(page.Comments), []string{liveComment, editedComment})
	if len(page.Projects) != 1 || page.Projects[0].ID != project {
		t.Fatalf("first page projects = %+v, want %s", page.Projects, project)
	}
	if page.Done || page.NextAfterNumber != 1 {
		t.Fatalf("first page done=%v next=%d, want more after number 1", page.Done, page.NextAfterNumber)
	}
	if page.Issues[0].Identifier != "SIX-1" || page.Issues[0].SearchUpdatedAt == "" {
		t.Fatalf("issue record = %+v, want identifier SIX-1 and a precise updated_at", page.Issues[0])
	}

	page = testutil.Decode[SearchIndexSnapshotResponse](t, testHandler.GetSearchIndexSnapshot,
		searchIndexRequest(http.MethodGet, "/api/search-index/snapshot?after_number=1&limit=1", wsID, nil), http.StatusOK)
	assertIDs(t, "second page issues", issueIDs(page.Issues), []string{second})
	if len(page.Projects) != 0 {
		t.Fatalf("projects belong to the first page only, got %d", len(page.Projects))
	}
	page = testutil.Decode[SearchIndexSnapshotResponse](t, testHandler.GetSearchIndexSnapshot,
		searchIndexRequest(http.MethodGet, "/api/search-index/snapshot?after_number=2&limit=1", wsID, nil), http.StatusOK)
	if !page.Done || len(page.Issues) != 0 {
		t.Fatalf("last page = %+v, want done with no issues", page)
	}

	// Nothing was written after the manifest snapshot.
	quiet := searchIndexChanges(t, wsID, manifest.Cursor, 100)
	if len(quiet.Issues)+len(quiet.Comments)+len(quiet.Projects)+len(quiet.Deleted.Issues)+len(quiet.Deleted.Comments) != 0 || quiet.HasMore {
		t.Fatalf("changes right after the manifest = %+v, want none", quiet)
	}

	dbfx.Exec(t, `UPDATE issue SET title = 'First indexed issue renamed' WHERE id = $1`, first)
	dbfx.Exec(t, `UPDATE comment SET content = 'comment edited' WHERE id = $1`, editedComment)
	dbfx.Exec(t, `UPDATE comment SET content = '', deleted_at = now() WHERE id = $1`, liveComment)
	laterComment := dbfx.Comment(t, second, "comment on a doomed issue", testutil.Cols{"workspace_id": wsID})
	dbfx.Exec(t, `DELETE FROM issue WHERE id = $1`, second)
	third := dbfx.Issue(t, "Third indexed issue", testutil.Cols{"workspace_id": wsID})
	dbfx.Exec(t, `UPDATE project SET title = 'Indexed project renamed' WHERE id = $1`, project)

	changes := searchIndexChanges(t, wsID, quiet.Cursor, 100)
	assertIDs(t, "upserted issues", issueIDs(changes.Issues), []string{first, third})
	assertIDs(t, "upserted comments", commentIDs(changes.Comments), []string{editedComment})
	assertIDs(t, "deleted issues", sortedIDs(changes.Deleted.Issues...), []string{second})
	// The tombstone and the comment cascaded away with its issue both count as deleted.
	assertIDs(t, "deleted comments", sortedIDs(changes.Deleted.Comments...), []string{liveComment, laterComment})
	if len(changes.Projects) != 1 || changes.Projects[0].Title != "Indexed project renamed" {
		t.Fatalf("upserted projects = %+v, want the renamed project", changes.Projects)
	}
	for _, is := range changes.Issues {
		if is.ID == first && is.Title != "First indexed issue renamed" {
			t.Fatalf("upserted issue carries stale title %q", is.Title)
		}
	}

	settled := searchIndexChanges(t, wsID, changes.Cursor, 100)
	if len(settled.Issues)+len(settled.Comments)+len(settled.Deleted.Issues)+len(settled.Deleted.Comments) != 0 {
		t.Fatalf("second catch-up = %+v, want nothing new", settled)
	}
}

func TestSearchIndexChangesPagesThroughKeyset(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := searchIndexWorkspace(t, "SIP")
	issue := dbfx.Issue(t, "Paged issue", testutil.Cols{"workspace_id": wsID})
	cursor := searchIndexManifest(t, wsID).Cursor

	var want []string
	for i := 0; i < 5; i++ {
		want = append(want, dbfx.Comment(t, issue, fmt.Sprintf("paged comment %d", i), testutil.Cols{"workspace_id": wsID}))
	}

	seen := map[string]int{}
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("catch-up did not terminate")
		}
		resp := searchIndexChanges(t, wsID, cursor, 2)
		for _, c := range resp.Comments {
			seen[c.ID]++
		}
		cursor = resp.Cursor
		if !resp.HasMore {
			break
		}
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Fatalf("comment %s delivered %d times across pages, want once (seen=%v)", id, seen[id], seen)
		}
	}
}

func TestSearchIndexChangesScopeToWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := searchIndexWorkspace(t, "SIA")
	otherWS := searchIndexWorkspace(t, "SIB")
	cursor := searchIndexManifest(t, wsID).Cursor
	dbfx.Issue(t, "Other workspace issue", testutil.Cols{"workspace_id": otherWS})

	resp := searchIndexChanges(t, wsID, cursor, 100)
	if len(resp.Issues) != 0 || len(resp.Deleted.Issues) != 0 {
		t.Fatalf("changes leaked another workspace: %+v", resp)
	}
}

// A writer that took its xid before the reader's snapshot but committed after
// it must still be delivered by the next catch-up. A watermark such as
// updated_at or the snapshot xmin alone would skip or stall on it.
func TestSearchIndexChangesDeliverLateCommits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID := searchIndexWorkspace(t, "SIL")
	issue := dbfx.Issue(t, "Late commit issue", testutil.Cols{"workspace_id": wsID})
	cursor := searchIndexManifest(t, wsID).Cursor

	slow, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin slow writer: %v", err)
	}
	defer slow.Rollback(ctx)
	var lateID string
	if err := slow.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'late comment', 'comment') RETURNING id
	`, issue, wsID, testUserID).Scan(&lateID); err != nil {
		t.Fatalf("insert late comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, lateID) })
	early := dbfx.Comment(t, issue, "early comment", testutil.Cols{"workspace_id": wsID})

	first := searchIndexChanges(t, wsID, cursor, 100)
	assertIDs(t, "comments before the slow commit", commentIDs(first.Comments), []string{early})

	if err := slow.Commit(ctx); err != nil {
		t.Fatalf("commit slow writer: %v", err)
	}
	second := searchIndexChanges(t, wsID, first.Cursor, 100)
	assertIDs(t, "comments after the slow commit", commentIDs(second.Comments), []string{lateID})
}

func TestSearchIndexChangesExpireAfterPruning(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID := searchIndexWorkspace(t, "SIE")
	cursor := searchIndexManifest(t, wsID).Cursor

	var previous *string
	_ = testPool.QueryRow(ctx, `SELECT pruned_through_xid::text FROM search_index_prune_mark`).Scan(&previous)
	t.Cleanup(func() {
		if previous == nil {
			testPool.Exec(context.Background(), `DELETE FROM search_index_prune_mark`)
			return
		}
		testPool.Exec(context.Background(), `UPDATE search_index_prune_mark SET pruned_through_xid = $1::xid8`, *previous)
	})
	dbfx.Exec(t, `
		INSERT INTO search_index_prune_mark (singleton, pruned_through_xid)
		VALUES (TRUE, pg_current_xact_id())
		ON CONFLICT (singleton) DO UPDATE SET pruned_through_xid = EXCLUDED.pruned_through_xid
	`)

	testutil.Call(t, testHandler.ListSearchIndexChanges,
		searchIndexRequest(http.MethodPost, "/api/search-index/changes", wsID, map[string]any{"cursor": cursor})).
		Want(http.StatusGone)
}

func TestSearchIndexChangesRejectInvalidCursors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	encode := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	for name, cursor := range map[string]string{
		"not base64":        "%%%",
		"wrong version":     encode(map[string]any{"v": 9, "s": "1:1:"}),
		"malformed since":   encode(map[string]any{"v": 1, "s": "1;DROP TABLE"}),
		"xmin above xmax":   encode(map[string]any{"v": 1, "s": "9:3:"}),
		"zero xmin":         encode(map[string]any{"v": 1, "s": "0:3:"}),
		"xip below xmin":    encode(map[string]any{"v": 1, "s": "3:9:1"}),
		"unsorted xips":     encode(map[string]any{"v": 1, "s": "3:9:5,4"}),
		"partial keyset":    encode(map[string]any{"v": 1, "s": "1:1:", "x": "5"}),
		"bad keyset type":   encode(map[string]any{"v": 1, "s": "1:1:", "t": "1:2:", "x": "1", "e": "user", "i": "00000000-0000-0000-0000-000000000000"}),
		"missing keyset id": encode(map[string]any{"v": 1, "s": "1:1:", "t": "1:2:", "x": "1", "e": "issue"}),
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.ListSearchIndexChanges,
				newRequest(http.MethodPost, "/api/search-index/changes", map[string]any{"cursor": cursor})).
				Want(http.StatusBadRequest)
		})
	}
}

func TestSearchIndexTriggersIgnoreUnsearchableCommentUpdates(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := searchIndexWorkspace(t, "SIR")
	issue := dbfx.Issue(t, "Revision issue", testutil.Cols{"workspace_id": wsID})
	comment := dbfx.Comment(t, issue, "revised", testutil.Cols{"workspace_id": wsID})
	cursor := searchIndexManifest(t, wsID).Cursor

	dbfx.Exec(t, `UPDATE comment SET revision = revision + 1 WHERE id = $1`, comment)
	resp := searchIndexChanges(t, wsID, cursor, 100)
	if len(resp.Comments) != 0 {
		t.Fatalf("a revision bump changes nothing searchable, got %+v", resp.Comments)
	}
}

func TestDeleteWorkspace_ClearsSearchIndexChanges(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const slug = "handler-tests-delete-search-index"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description) VALUES ('Search Index Teardown', $1, '') RETURNING id
	`, slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM search_index_change WHERE workspace_id = $1`, wsID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}
	issue := dbfx.Issue(t, "Teardown issue", testutil.Cols{"workspace_id": wsID})
	dbfx.Comment(t, issue, "teardown comment", testutil.Cols{"workspace_id": wsID})
	if n := dbfx.Count(t, `SELECT count(*) FROM search_index_change WHERE workspace_id = $1`, wsID); n == 0 {
		t.Fatal("writes should have logged search index changes")
	}

	w := httptest.NewRecorder()
	testHandler.DeleteWorkspace(w, withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+wsID, nil), "id", wsID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM search_index_change WHERE workspace_id = $1`, wsID); n != 0 {
		t.Fatalf("workspace teardown left %d search index change rows", n)
	}
}

func TestRequireLocalSearchIndexHonorsKillSwitch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	guarded := testHandler.RequireLocalSearchIndex(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testutil.Call(t, guarded.ServeHTTP, newRequest(http.MethodGet, "/api/search-index/manifest", nil)).Want(http.StatusOK)

	withFeatureFlag(t, testHandler, featureflags.LocalSearchIndex, false)
	testutil.Call(t, guarded.ServeHTTP, newRequest(http.MethodGet, "/api/search-index/manifest", nil)).Want(http.StatusNotFound)
}

func TestSearchIndexBootstrapReadsShedLoadWhenSaturated(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := searchIndexWorkspace(t, "SIS")
	// Occupy every slot, as a fleet of clients bootstrapping at once would.
	for i := 0; i < cap(searchIndexBootstrapSlots); i++ {
		searchIndexBootstrapSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(searchIndexBootstrapSlots); i++ {
			<-searchIndexBootstrapSlots
		}
	})

	resp := testutil.Call(t, testHandler.GetSearchIndexManifest,
		searchIndexRequest(http.MethodGet, "/api/search-index/manifest", wsID, nil)).Want(http.StatusServiceUnavailable)
	if resp.Header().Get("Retry-After") == "" {
		t.Fatal("a shed request must tell the client when to retry")
	}
	testutil.Call(t, testHandler.GetSearchIndexSnapshot,
		searchIndexRequest(http.MethodGet, "/api/search-index/snapshot", wsID, nil)).Want(http.StatusServiceUnavailable)
}

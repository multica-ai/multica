package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func createDocForTest(t *testing.T) DocResponse {
	t.Helper()
	var doc DocResponse
	testutil.Call(t, testHandler.CreateDoc, newRequest("POST", "/api/docs", map[string]any{"title": "Runbook", "content": "# First\n\nOriginal"})).Want(http.StatusCreated).JSON(&doc)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM doc_version WHERE doc_id = $1", doc.ID)
		testPool.Exec(context.Background(), "DELETE FROM doc WHERE id = $1", doc.ID)
	})
	return doc
}

func TestDocSnapshotsConflictRestoreAndIsolation(t *testing.T) {
	doc := createDocForTest(t)
	var updated DocResponse
	patch := func(body map[string]any) *http.Request {
		return withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, body), "id", doc.ID)
	}
	testutil.Call(t, testHandler.UpdateDoc, patch(map[string]any{"base_revision": 1, "title": "Changed", "content": "Second"})).Want(http.StatusOK).JSON(&updated)
	if updated.Revision != 2 || updated.VersionCount != 2 {
		t.Fatalf("each save must append: %+v", updated)
	}
	var conflict map[string]any
	testutil.Call(t, testHandler.UpdateDoc, patch(map[string]any{"base_revision": 1, "content": "Stale"})).Want(http.StatusConflict).JSON(&conflict)
	if conflict["code"] != "revision_conflict" {
		t.Fatalf("unexpected conflict: %v", conflict)
	}
	testutil.Call(t, testHandler.UpdateDoc, patch(map[string]any{"base_revision": 2, "action": "restore", "action_ref": 1, "content": "ignored", "title": "ignored"})).Want(http.StatusOK).JSON(&updated)
	if updated.Revision != 3 || updated.VersionCount != 3 || updated.Title != doc.Title || updated.Content != doc.Content {
		t.Fatalf("restore must copy title/body into a new snapshot: %+v", updated)
	}
	var history struct {
		Versions []DocVersionResponse `json:"versions"`
	}
	testutil.Call(t, testHandler.ListDocVersions, withURLParam(newRequest("GET", "/api/docs/versions", nil), "id", doc.ID)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 3 {
		t.Fatalf("history overwritten: %v", history.Versions)
	}
	req := withURLParam(newRequest("GET", "/api/docs/versions/1", nil), "id", doc.ID)
	req = withURLParams(req, "id", doc.ID, "number", "1")
	var version DocVersionResponse
	testutil.Call(t, testHandler.GetDocVersion, req).Want(http.StatusOK).JSON(&version)
	if version.Content == nil || *version.Content != doc.Content || version.Title != doc.Title {
		t.Fatalf("original snapshot changed: %+v", version)
	}
	for _, handler := range []http.HandlerFunc{testHandler.GetDoc, testHandler.ListDocVersions, testHandler.UpdateDoc, testHandler.DeleteDoc} {
		req := patch(map[string]any{"base_revision": 3, "content": "Cross tenant"})
		req.Header.Set("X-Workspace-ID", "00000000-0000-4000-8000-000000000099")
		testutil.Call(t, handler, req).Want(http.StatusNotFound)
	}
	testutil.Call(t, testHandler.UpdateDoc, patch(map[string]any{"content": "No revision"})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.UpdateDoc, patch(map[string]any{"base_revision": 3, "action": "revert", "action_ref": 1})).Want(http.StatusBadRequest)
}

func TestDocDeleteHumanOnlyAndHistoryCleanup(t *testing.T) {
	doc := createDocForTest(t)
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := withURLParam(newRequest("DELETE", "/api/docs", nil), "id", doc.ID)
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.DeleteDoc, req).Want(http.StatusForbidden)
	}
	req := withURLParam(newRequest("DELETE", "/api/docs", nil), "id", doc.ID)
	req.Header.Set("X-Agent-ID", "00000000-0000-4000-8000-000000000001")
	testutil.Call(t, testHandler.DeleteDoc, req).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteDoc, withURLParam(newRequest("DELETE", "/api/docs", nil), "id", doc.ID)).Want(http.StatusNoContent)
	var count int
	dbfx.QueryRow(t, "SELECT count(*) FROM doc_version WHERE doc_id = $1", doc.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("delete left %d snapshots", count)
	}
}

func TestDocFolderCycleAndNonemptyDelete(t *testing.T) {
	create := func(parent *string) DocFolderResponse {
		var f DocFolderResponse
		testutil.Call(t, testHandler.CreateDocFolder, newRequest("POST", "/api/doc-folders", map[string]any{"title": "Guides", "parent_id": parent})).Want(http.StatusCreated).JSON(&f)
		t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM doc_folder WHERE id = $1", f.ID) })
		return f
	}
	root := create(nil)
	child := create(&root.ID)
	testutil.Call(t, testHandler.MoveDocFolder, withURLParam(newRequest("POST", "/move", map[string]any{"parent_id": child.ID}), "id", root.ID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DeleteDocFolder, withURLParam(newRequest("DELETE", "/folder", nil), "id", root.ID)).Want(http.StatusConflict)
	req := withURLParam(newRequest("DELETE", "/folder", nil), "id", child.ID)
	req.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.DeleteDocFolder, req).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.MoveDocFolder, withURLParam(newRequest("POST", "/move", map[string]any{"parent_id": nil}), "id", child.ID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.DeleteDocFolder, withURLParam(newRequest("DELETE", "/folder", nil), "id", root.ID)).Want(http.StatusNoContent)
}

func TestDocEditsMergeIntoOneVersionPerEditingStretch(t *testing.T) {
	doc := createDocForTest(t)
	var updated DocResponse
	revision := int64(1)
	save := func(body map[string]any) DocResponse {
		t.Helper()
		body["base_revision"] = revision
		req := withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, body), "id", doc.ID)
		testutil.Call(t, testHandler.UpdateDoc, req).Want(http.StatusOK).JSON(&updated)
		revision = updated.Revision
		return updated
	}
	// The create snapshot stands alone; the first edit opens a new one.
	if got := save(map[string]any{"content": "a"}); got.VersionCount != 2 {
		t.Fatalf("first edit must open a version: %+v", got)
	}
	// Further autosaves in the same stretch fold into it.
	save(map[string]any{"content": "ab"})
	if got := save(map[string]any{"title": "Renamed", "content": "abc"}); got.VersionCount != 2 || got.Revision != 4 {
		t.Fatalf("autosaves must merge but still bump the revision: %+v", got)
	}
	var version DocVersionResponse
	req := withURLParams(newRequest("GET", "/api/docs/versions/2", nil), "id", doc.ID, "number", "2")
	testutil.Call(t, testHandler.GetDocVersion, req).Want(http.StatusOK).JSON(&version)
	if version.Content == nil || *version.Content != "abc" || version.Title != "Renamed" {
		t.Fatalf("merged snapshot must hold the latest save: %+v", version)
	}
	// A labeled save is its own version.
	if got := save(map[string]any{"content": "abcd", "message": "Checkpoint"}); got.VersionCount != 3 {
		t.Fatalf("labeled save must append: %+v", got)
	}
	// So is the next stretch, once the open snapshot is older than the window.
	if got := save(map[string]any{"content": "abcde"}); got.VersionCount != 4 {
		t.Fatalf("edit after a labeled save must append: %+v", got)
	}
	testPool.Exec(context.Background(), "UPDATE doc_version SET created_at = now() - interval '20 minutes' WHERE doc_id = $1 AND number = 4", doc.ID)
	if got := save(map[string]any{"content": "abcde!"}); got.VersionCount != 4 {
		t.Fatalf("edit inside the 30-minute window must merge: %+v", got)
	}
	testPool.Exec(context.Background(), "UPDATE doc_version SET created_at = now() - interval '31 minutes' WHERE doc_id = $1 AND number = 4", doc.ID)
	if got := save(map[string]any{"content": "abcdef"}); got.VersionCount != 5 {
		t.Fatalf("edit after the window must append: %+v", got)
	}
	// A restore never absorbs later edits.
	save(map[string]any{"action": "restore", "action_ref": 1})
	if got := save(map[string]any{"content": "after restore"}); got.VersionCount != 7 {
		t.Fatalf("edit after a restore must append: %+v", got)
	}
}

// A NUL survives the JSON round trip but PostgreSQL TEXT rejects it, so an
// agent saving tool output that carries one got a 500 and wrote nothing.
func TestDocTextDropsNULBytes(t *testing.T) {
	var doc DocResponse
	testutil.Call(t, testHandler.CreateDoc, newRequest("POST", "/api/docs", map[string]any{
		"title": "Run\x00book", "content": "Step\x00 one", "message": "Im\x00port",
	})).Want(http.StatusCreated).JSON(&doc)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM doc_version WHERE doc_id = $1", doc.ID)
		testPool.Exec(context.Background(), "DELETE FROM doc WHERE id = $1", doc.ID)
	})
	if doc.Title != "Runbook" || doc.Content != "Step one" {
		t.Fatalf("create must drop NUL bytes: %+v", doc)
	}

	var updated DocResponse
	req := withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{
		"base_revision": 1, "title": "Run\x00book 2", "content": "Step\x00 two", "message": "Re\x00vise",
	}), "id", doc.ID)
	testutil.Call(t, testHandler.UpdateDoc, req).Want(http.StatusOK).JSON(&updated)
	if updated.Title != "Runbook 2" || updated.Content != "Step two" {
		t.Fatalf("update must drop NUL bytes: %+v", updated)
	}
	var history struct {
		Versions []DocVersionResponse `json:"versions"`
	}
	testutil.Call(t, testHandler.ListDocVersions, withURLParam(newRequest("GET", "/api/docs/versions", nil), "id", doc.ID)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 2 || history.Versions[0].Message != "Revise" || history.Versions[1].Message != "Import" {
		t.Fatalf("history messages must drop NUL bytes: %+v", history.Versions)
	}

	var folder DocFolderResponse
	testutil.Call(t, testHandler.CreateDocFolder, newRequest("POST", "/api/doc-folders", map[string]any{"title": "Gui\x00des"})).Want(http.StatusCreated).JSON(&folder)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM doc_folder WHERE id = $1", folder.ID) })
	if folder.Title != "Guides" {
		t.Fatalf("folder create must drop NUL bytes: %+v", folder)
	}
	testutil.Call(t, testHandler.UpdateDocFolder, withURLParam(newRequest("PATCH", "/api/doc-folders/"+folder.ID, map[string]any{"title": "Ref\x00s"}), "id", folder.ID)).Want(http.StatusOK).JSON(&folder)
	if folder.Title != "Refs" {
		t.Fatalf("folder rename must drop NUL bytes: %+v", folder)
	}
}

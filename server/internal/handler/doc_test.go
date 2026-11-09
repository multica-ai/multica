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

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func cleanupDocTestRows(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM doc_version WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM doc_read WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM project_doc WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM doc WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM doc_folder WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func createDocForTest(t *testing.T, body map[string]any) DocResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateDoc(w, newRequest("POST", "/api/docs", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create doc: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var doc DocResponse
	json.NewDecoder(w.Body).Decode(&doc)
	return doc
}

func patchDocForTest(t *testing.T, id string, body map[string]any) (DocResponse, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.UpdateDoc(w, withURLParams(newRequest("PATCH", "/api/docs/"+id, body), "id", id))
	var doc DocResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&doc)
	}
	return doc, w
}

func listDocVersionsForTest(t *testing.T, id string) []DocVersionResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListDocVersions(w, withURLParams(newRequest("GET", "/api/docs/"+id+"/versions", nil), "id", id))
	if w.Code != http.StatusOK {
		t.Fatalf("list versions: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Versions []DocVersionResponse `json:"versions"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp.Versions
}

func TestDocCreateUpdateAndConflict(t *testing.T) {
	cleanupDocTestRows(t)

	doc := createDocForTest(t, map[string]any{"title": "Runbook", "content": "# Runbook\n\nstep one"})
	if doc.Revision != 1 || doc.VersionCount != 1 || doc.Format != "markdown" {
		t.Fatalf("unexpected created doc: %+v", doc.DocSummaryResponse)
	}
	if doc.LastSeenAt == nil {
		t.Fatalf("creator should have a read mark")
	}

	// A member's follow-up edit folds into the creation session.
	updated, w := patchDocForTest(t, doc.ID, map[string]any{"content": "# Runbook\n\nstep one\nstep two", "base_revision": 1})
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if updated.Revision != 2 || updated.VersionCount != 1 {
		t.Fatalf("expected coalesced write (revision 2, 1 version), got revision %d versions %d", updated.Revision, updated.VersionCount)
	}

	// A stale base revision is refused with the current revision.
	_, w = patchDocForTest(t, doc.ID, map[string]any{"content": "lost", "base_revision": 1})
	if w.Code != http.StatusConflict {
		t.Fatalf("stale write: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflict map[string]any
	json.NewDecoder(w.Body).Decode(&conflict)
	if conflict["code"] != "revision_conflict" || conflict["actual_revision"] != float64(2) {
		t.Fatalf("unexpected conflict body: %v", conflict)
	}

	// A message always starts a new version.
	updated, w = patchDocForTest(t, doc.ID, map[string]any{"content": "# Runbook\n\nstep one\nstep 2", "base_revision": 2, "message": "fix numbering"})
	if w.Code != http.StatusOK || updated.VersionCount != 2 || updated.Revision != 3 {
		t.Fatalf("message write: got %d revision %d versions %d: %s", w.Code, updated.Revision, updated.VersionCount, w.Body.String())
	}

	// A no-op write keeps the revision.
	same, w := patchDocForTest(t, doc.ID, map[string]any{"content": "# Runbook\n\nstep one\nstep 2", "base_revision": 3})
	if w.Code != http.StatusOK || same.Revision != 3 {
		t.Fatalf("no-op write should not bump revision: %d %d", w.Code, same.Revision)
	}

	versions := listDocVersionsForTest(t, doc.ID)
	if len(versions) != 2 || versions[0].Number != 2 || versions[0].Message != "fix numbering" || versions[1].Action != "create" {
		t.Fatalf("unexpected versions: %+v", versions)
	}
	if versions[0].LinesAdded != 1 || versions[0].LinesRemoved != 1 {
		t.Fatalf("expected +1 -1 on v2, got +%d -%d", versions[0].LinesAdded, versions[0].LinesRemoved)
	}
	if versions[1].LinesAdded != 3 {
		t.Fatalf("coalesced v1 should count all lines of its session, got +%d", versions[1].LinesAdded)
	}
}

func TestDocRestoreAndRevertAppendVersions(t *testing.T) {
	cleanupDocTestRows(t)

	doc := createDocForTest(t, map[string]any{"title": "Spec", "content": "a\nb", "message": "draft"})
	updated, w := patchDocForTest(t, doc.ID, map[string]any{"content": "a\nb\nc", "base_revision": 1, "message": "add c"})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	restored, w := patchDocForTest(t, doc.ID, map[string]any{"action": "restore", "action_ref": 1, "base_revision": updated.Revision})
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	if restored.Content != "a\nb" || restored.VersionCount != 3 {
		t.Fatalf("restore should append v3 with v1 content, got v%d %q", restored.VersionCount, restored.Content)
	}

	_, w = patchDocForTest(t, doc.ID, map[string]any{"action": "revert", "action_ref": 9, "content": "x", "base_revision": restored.Revision})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("revert of a missing version: expected 400, got %d", w.Code)
	}

	versions := listDocVersionsForTest(t, doc.ID)
	if versions[0].Action != "restore" || versions[0].ActionRef == nil || *versions[0].ActionRef != 1 {
		t.Fatalf("expected restore of v1 on top, got %+v", versions[0])
	}

	w2 := httptest.NewRecorder()
	testHandler.GetDocVersion(w2, withURLParams(newRequest("GET", "/", nil), "id", doc.ID, "number", "2"))
	var v2 DocVersionResponse
	json.NewDecoder(w2.Body).Decode(&v2)
	if w2.Code != http.StatusOK || v2.Content == nil || *v2.Content != "a\nb\nc" {
		t.Fatalf("version 2 content: %d %+v", w2.Code, v2)
	}
}

func TestShouldCoalesceDocVersion(t *testing.T) {
	now := time.Now()
	actor := "11111111-1111-1111-1111-111111111111"
	latest := db.DocVersion{
		AuthorType: "member",
		AuthorID:   parseUUID(actor),
		Action:     "edit",
		CreatedAt:  pgtype.Timestamptz{Time: now.Add(-20 * time.Minute), Valid: true},
		UpdatedAt:  pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
	}
	if !shouldCoalesceDocVersion(latest, "member", actor, "edit", "", now) {
		t.Fatal("live session should coalesce")
	}
	if shouldCoalesceDocVersion(latest, "agent", actor, "edit", "", now) {
		t.Fatal("agents never coalesce")
	}
	if shouldCoalesceDocVersion(latest, "member", actor, "edit", "note", now) {
		t.Fatal("a message starts a new version")
	}
	idle := latest
	idle.UpdatedAt = pgtype.Timestamptz{Time: now.Add(-11 * time.Minute), Valid: true}
	if shouldCoalesceDocVersion(idle, "member", actor, "edit", "", now) {
		t.Fatal("an idle session should not coalesce")
	}
	long := latest
	long.CreatedAt = pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true}
	if shouldCoalesceDocVersion(long, "member", actor, "edit", "", now) {
		t.Fatal("a session past the cap should not coalesce")
	}
	restored := latest
	restored.Action = "restore"
	if shouldCoalesceDocVersion(restored, "member", actor, "edit", "", now) {
		t.Fatal("edits after a restore start a new version")
	}
}

func TestDocFoldersNestMoveAndDelete(t *testing.T) {
	cleanupDocTestRows(t)

	createFolder := func(title string, parent *string) DocFolderResponse {
		t.Helper()
		body := map[string]any{"title": title}
		if parent != nil {
			body["parent_id"] = *parent
		}
		w := httptest.NewRecorder()
		testHandler.CreateDocFolder(w, newRequest("POST", "/api/doc-folders", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create folder: %d %s", w.Code, w.Body.String())
		}
		var f DocFolderResponse
		json.NewDecoder(w.Body).Decode(&f)
		return f
	}
	moveFolder := func(id string, parent any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.MoveDocFolder(w, withURLParams(newRequest("POST", "/", map[string]any{"parent_id": parent}), "id", id))
		return w
	}

	root := createFolder("Engineering", nil)
	child := createFolder("Backend", &root.ID)
	grandchild := createFolder("Runbooks", &child.ID)
	if grandchild.ParentID == nil || *grandchild.ParentID != child.ID {
		t.Fatalf("expected nested folder, got %+v", grandchild)
	}

	if w := moveFolder(root.ID, grandchild.ID); w.Code != http.StatusBadRequest {
		t.Fatalf("cycle move: expected 400, got %d %s", w.Code, w.Body.String())
	}
	if w := moveFolder(root.ID, root.ID); w.Code != http.StatusBadRequest {
		t.Fatalf("self move: expected 400, got %d", w.Code)
	}
	if w := moveFolder(grandchild.ID, nil); w.Code != http.StatusOK {
		t.Fatalf("move to root: expected 200, got %d %s", w.Code, w.Body.String())
	}

	doc := createDocForTest(t, map[string]any{"title": "Deploy", "folder_id": child.ID})
	w := httptest.NewRecorder()
	testHandler.DeleteDocFolder(w, withURLParams(newRequest("DELETE", "/", nil), "id", child.ID))
	if w.Code != http.StatusConflict {
		t.Fatalf("delete non-empty folder: expected 409, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.MoveDoc(w, withURLParams(newRequest("POST", "/", map[string]any{"folder_id": grandchild.ID}), "id", doc.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("move doc: %d %s", w.Code, w.Body.String())
	}
	var moved DocSummaryResponse
	json.NewDecoder(w.Body).Decode(&moved)
	if moved.FolderID == nil || *moved.FolderID != grandchild.ID || moved.Revision != doc.Revision {
		t.Fatalf("move should change folder only: %+v", moved)
	}

	w = httptest.NewRecorder()
	testHandler.DeleteDocFolder(w, withURLParams(newRequest("DELETE", "/", nil), "id", child.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete empty folder: expected 204, got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ListDocTree(w, newRequest("GET", "/api/docs", nil))
	var tree struct {
		Folders []DocFolderResponse  `json:"folders"`
		Docs    []DocSummaryResponse `json:"docs"`
	}
	json.NewDecoder(w.Body).Decode(&tree)
	if len(tree.Folders) != 2 || len(tree.Docs) != 1 {
		t.Fatalf("tree: expected 2 folders and 1 doc, got %d/%d", len(tree.Folders), len(tree.Docs))
	}
}

func TestProjectDocLinks(t *testing.T) {
	cleanupDocTestRows(t)

	var projectID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO project (workspace_id, title) VALUES ($1, 'KB link test') RETURNING id`, testWorkspaceID,
	).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	doc := createDocForTest(t, map[string]any{"title": "Design"})
	link := func() int {
		w := httptest.NewRecorder()
		testHandler.LinkProjectDoc(w, withURLParams(newRequest("POST", "/", map[string]any{"doc_id": doc.ID}), "id", projectID))
		return w.Code
	}
	if code := link(); code != http.StatusCreated {
		t.Fatalf("link: expected 201, got %d", code)
	}
	if code := link(); code != http.StatusOK {
		t.Fatalf("relink: expected 200, got %d", code)
	}

	w := httptest.NewRecorder()
	testHandler.GetDoc(w, withURLParams(newRequest("GET", "/", nil), "id", doc.ID))
	var got DocResponse
	json.NewDecoder(w.Body).Decode(&got)
	if len(got.ProjectIDs) != 1 || got.ProjectIDs[0] != projectID {
		t.Fatalf("doc should list its project, got %v", got.ProjectIDs)
	}

	w = httptest.NewRecorder()
	testHandler.UnlinkProjectDoc(w, withURLParams(newRequest("DELETE", "/", nil), "id", projectID, "docId", doc.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("unlink: expected 204, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.ListProjectDocs(w, withURLParams(newRequest("GET", "/", nil), "id", projectID))
	var list struct {
		Docs []DocSummaryResponse `json:"docs"`
	}
	json.NewDecoder(w.Body).Decode(&list)
	if len(list.Docs) != 0 {
		t.Fatalf("expected no linked docs after unlink, got %d", len(list.Docs))
	}
}

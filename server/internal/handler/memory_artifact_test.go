package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestMemoryArtifactLifecycle: create → get → list → update → archive →
// list-default-hides → list-include_archived-shows → restore →
// re-archive 409 path → delete.
func TestMemoryArtifactLifecycle(t *testing.T) {
	// 1. Create.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":    "wiki_page",
		"title":   "Lifecycle Test",
		"content": "# Hello\n\nA wiki page.",
		"slug":    "lifecycle-test",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMemoryArtifact: %d %s", w.Code, w.Body.String())
	}
	var created MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Kind != "wiki_page" || created.Title != "Lifecycle Test" {
		t.Fatalf("unexpected created body: %+v", created)
	}
	if created.Slug == nil || *created.Slug != "lifecycle-test" {
		t.Fatalf("slug round-trip failed: %+v", created.Slug)
	}
	if created.ArchivedAt != nil {
		t.Fatalf("freshly-created artifact should not be archived")
	}
	defer func() {
		req := newRequest("DELETE", "/api/memory/"+created.ID, nil)
		req = withURLParam(req, "id", created.ID)
		testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
	}()

	// 2. Get by id.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetMemoryArtifact: %d %s", w.Code, w.Body.String())
	}

	// 3. Update content.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/memory/"+created.ID, map[string]any{
		"content": "# Updated",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMemoryArtifact: %d %s", w.Code, w.Body.String())
	}
	var updated MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Content != "# Updated" {
		t.Fatalf("update did not stick: %+v", updated.Content)
	}

	// 4. Archive.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory/"+created.ID+"/archive", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ArchiveMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveMemoryArtifact: %d %s", w.Code, w.Body.String())
	}
	var archived MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&archived)
	if archived.ArchivedAt == nil {
		t.Fatalf("expected archived_at to be stamped")
	}

	// 5. Default list — hidden.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory?workspace_id="+testWorkspaceID, nil)
	testHandler.ListMemoryArtifacts(w, req)
	var defaultList struct {
		Artifacts []MemoryArtifactResponse `json:"memory_artifacts"`
	}
	json.NewDecoder(w.Body).Decode(&defaultList)
	for _, a := range defaultList.Artifacts {
		if a.ID == created.ID {
			t.Fatalf("archived artifact still visible in default list")
		}
	}

	// 6. include_archived=true — visible.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory?workspace_id="+testWorkspaceID+"&include_archived=true", nil)
	testHandler.ListMemoryArtifacts(w, req)
	var inclList struct {
		Artifacts []MemoryArtifactResponse `json:"memory_artifacts"`
	}
	json.NewDecoder(w.Body).Decode(&inclList)
	found := false
	for _, a := range inclList.Artifacts {
		if a.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("archived artifact missing from include_archived list")
	}

	// 7. Re-archive must be 409.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory/"+created.ID+"/archive", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ArchiveMemoryArtifact(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("re-archive: expected 409, got %d", w.Code)
	}

	// 8. Restore.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory/"+created.ID+"/restore", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.RestoreMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RestoreMemoryArtifact: %d %s", w.Code, w.Body.String())
	}

	// 9. Restore-non-archived must be 409.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory/"+created.ID+"/restore", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.RestoreMemoryArtifact(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("restore non-archived: expected 409, got %d", w.Code)
	}
}

// TestMemoryArtifactSlugUniqueness verifies the (workspace, kind, slug)
// uniqueness constraint surfaces as a clean 409.
func TestMemoryArtifactSlugUniqueness(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":  "runbook",
		"title": "Deploy",
		"slug":  "deploy",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", w.Code, w.Body.String())
	}
	var first MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&first)
	defer func() {
		req := newRequest("DELETE", "/api/memory/"+first.ID, nil)
		req = withURLParam(req, "id", first.ID)
		testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
	}()

	// Same slug + same kind in same workspace → 409.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":  "runbook",
		"title": "Another deploy",
		"slug":  "deploy",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on slug collision, got %d %s", w.Code, w.Body.String())
	}

	// Same slug but DIFFERENT kind in same workspace → allowed
	// (uniqueness is per kind, not per workspace).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":  "wiki_page",
		"title": "Deploy doc",
		"slug":  "deploy",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("cross-kind same-slug should succeed, got %d %s", w.Code, w.Body.String())
	}
	var second MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&second)
	defer func() {
		req := newRequest("DELETE", "/api/memory/"+second.ID, nil)
		req = withURLParam(req, "id", second.ID)
		testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
	}()
}

// TestMemoryArtifactByAnchor covers the anchor lookup that powers
// runtime context injection. Two artifacts anchored to the same issue
// are returned; an artifact anchored to a different issue is not.
func TestMemoryArtifactByAnchor(t *testing.T) {
	// Create an issue to anchor against.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Anchor target issue",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	defer func() {
		req := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		req = withURLParam(req, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	}()

	// And a second issue we should NOT pick up.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Other issue",
	})
	testHandler.CreateIssue(w, req)
	var other IssueResponse
	json.NewDecoder(w.Body).Decode(&other)
	defer func() {
		req := newRequest("DELETE", "/api/issues/"+other.ID, nil)
		req = withURLParam(req, "id", other.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	}()

	// Two artifacts anchored to issue, one to other.
	mkAnchored := func(title, anchorIssueID string) MemoryArtifactResponse {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
			"kind":        "agent_note",
			"title":       title,
			"anchor_type": "issue",
			"anchor_id":   anchorIssueID,
		})
		testHandler.CreateMemoryArtifact(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create anchored: %d %s", w.Code, w.Body.String())
		}
		var a MemoryArtifactResponse
		json.NewDecoder(w.Body).Decode(&a)
		t.Cleanup(func() {
			req := newRequest("DELETE", "/api/memory/"+a.ID, nil)
			req = withURLParam(req, "id", a.ID)
			testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
		})
		return a
	}
	a1 := mkAnchored("Note 1 about issue", issue.ID)
	a2 := mkAnchored("Note 2 about issue", issue.ID)
	_ = mkAnchored("Note about OTHER", other.ID)

	// Anchor lookup returns just the two notes for this issue.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/by-anchor/issue/"+issue.ID+"?workspace_id="+testWorkspaceID, nil)
	// Both URL params must live in the SAME chi.RouteContext — the
	// withURLParam helper creates a fresh context on each call, so two
	// chained calls would silently drop the first param. Inline the
	// chi route context so both params are visible to the handler.
	{
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("anchorType", "issue")
		rctx.URLParams.Add("anchorId", issue.ID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	testHandler.ListMemoryArtifactsByAnchor(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMemoryArtifactsByAnchor: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Artifacts []MemoryArtifactResponse `json:"memory_artifacts"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	gotIDs := map[string]bool{}
	for _, a := range resp.Artifacts {
		gotIDs[a.ID] = true
	}
	if !gotIDs[a1.ID] || !gotIDs[a2.ID] {
		t.Fatalf("anchor lookup missing one of the expected artifacts: got %+v", gotIDs)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("anchor lookup returned wrong count: got %d, want 2", len(resp.Artifacts))
	}
}

// TestMemoryArtifactSearch exercises the websearch_to_tsquery path —
// proves the GIN index works end-to-end.
func TestMemoryArtifactSearch(t *testing.T) {
	mk := func(title, content string) MemoryArtifactResponse {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
			"kind":    "wiki_page",
			"title":   title,
			"content": content,
		})
		testHandler.CreateMemoryArtifact(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var a MemoryArtifactResponse
		json.NewDecoder(w.Body).Decode(&a)
		t.Cleanup(func() {
			req := newRequest("DELETE", "/api/memory/"+a.ID, nil)
			req = withURLParam(req, "id", a.ID)
			testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
		})
		return a
	}
	hit := mk("OAuth migration plan", "Move auth from JWT to OAuth2 provider.")
	miss := mk("Database backup", "Daily snapshots stored in S3.")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/memory/search?workspace_id="+testWorkspaceID+"&q=oauth", nil)
	testHandler.SearchMemoryArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchMemoryArtifacts: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Artifacts []MemoryArtifactResponse `json:"memory_artifacts"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	hitFound := false
	for _, a := range resp.Artifacts {
		if a.ID == hit.ID {
			hitFound = true
		}
		if a.ID == miss.ID {
			t.Errorf("search returned unrelated artifact: %+v", a.Title)
		}
	}
	if !hitFound {
		t.Fatalf("search did not return the OAuth hit; got %+v", resp.Artifacts)
	}
}

// TestMemoryArtifactRejectsInvalidKind ensures the open-string SQL
// column doesn't let arbitrary kinds through — the service-layer
// allowlist is the gate.
func TestMemoryArtifactRejectsInvalidKind(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":  "arbitrary_kind",
		"title": "Should fail",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid kind, got %d %s", w.Code, w.Body.String())
	}
}

// TestMemoryArtifactRejectsMismatchedAnchor — both anchor fields must
// be set together.
func TestMemoryArtifactRejectsMismatchedAnchor(t *testing.T) {
	atype := "issue"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":        "agent_note",
		"title":       "Half anchor",
		"anchor_type": atype,
		// anchor_id intentionally omitted
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on half anchor, got %d %s", w.Code, w.Body.String())
	}
}

// TestMemoryArtifactRevisionHistory exercises the versioning path:
// create → update twice (each captures the prior state) → list history
// → fetch a specific revision → restore (which itself snapshots the
// current state, producing a third revision row).
func TestMemoryArtifactRevisionHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// 1. Create.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memory?workspace_id="+testWorkspaceID, map[string]any{
		"kind":    "wiki_page",
		"title":   "Revision Test v1",
		"content": "Original content.",
	})
	testHandler.CreateMemoryArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMemoryArtifact: %d %s", w.Code, w.Body.String())
	}
	var created MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		req := newRequest("DELETE", "/api/memory/"+created.ID, nil)
		req = withURLParam(req, "id", created.ID)
		testHandler.DeleteMemoryArtifact(httptest.NewRecorder(), req)
	}()

	// 2. First edit — should snapshot v1's state into revision 1.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/memory/"+created.ID, map[string]any{
		"title":   "Revision Test v2",
		"content": "Second draft content.",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first UpdateMemoryArtifact: %d %s", w.Code, w.Body.String())
	}

	// 3. Second edit — should snapshot v2's state into revision 2.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/memory/"+created.ID, map[string]any{
		"content": "Third draft content.",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateMemoryArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second UpdateMemoryArtifact: %d %s", w.Code, w.Body.String())
	}

	// 4. List history — expect two revisions, newest-first.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/"+created.ID+"/history", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ListMemoryArtifactRevisions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMemoryArtifactRevisions: %d %s", w.Code, w.Body.String())
	}
	var histResp struct {
		Revisions []MemoryArtifactRevisionSummary `json:"revisions"`
	}
	json.NewDecoder(w.Body).Decode(&histResp)
	if got := len(histResp.Revisions); got != 2 {
		t.Fatalf("expected 2 revisions, got %d: %+v", got, histResp.Revisions)
	}
	// Newest-first ordering: revision_number 2 first, then 1.
	if histResp.Revisions[0].RevisionNumber != 2 {
		t.Errorf("expected first listed revision_number=2, got %d", histResp.Revisions[0].RevisionNumber)
	}
	if histResp.Revisions[1].RevisionNumber != 1 {
		t.Errorf("expected second listed revision_number=1, got %d", histResp.Revisions[1].RevisionNumber)
	}
	// Revision 1 captures the v1 (original) state because the first
	// update happened on top of v1; revision 2 captures v2.
	if histResp.Revisions[1].Title != "Revision Test v1" {
		t.Errorf("rev1 title: got %q want 'Revision Test v1'", histResp.Revisions[1].Title)
	}
	if histResp.Revisions[0].Title != "Revision Test v2" {
		t.Errorf("rev2 title: got %q want 'Revision Test v2'", histResp.Revisions[0].Title)
	}

	// 5. Get full content of revision 1 — should be the original content.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/"+created.ID+"/history/1", nil)
	req = withURLParam(req, "id", created.ID)
	req = withURLParam(req, "revision", "1")
	testHandler.GetMemoryArtifactRevision(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetMemoryArtifactRevision: %d %s", w.Code, w.Body.String())
	}
	var revDetail MemoryArtifactRevisionDetail
	json.NewDecoder(w.Body).Decode(&revDetail)
	if revDetail.Content != "Original content." {
		t.Errorf("rev1 content: got %q want 'Original content.'", revDetail.Content)
	}

	// 6. Restore to revision 1 — should snapshot the current (v3) state
	// as revision 3, then apply v1's content onto the live row.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memory/"+created.ID+"/restore-revision/1", map[string]any{})
	req = withURLParam(req, "id", created.ID)
	req = withURLParam(req, "revision", "1")
	testHandler.RestoreMemoryArtifactRevision(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RestoreMemoryArtifactRevision: %d %s", w.Code, w.Body.String())
	}
	var restored MemoryArtifactResponse
	json.NewDecoder(w.Body).Decode(&restored)
	if restored.Content != "Original content." {
		t.Errorf("after restore live content: got %q want 'Original content.'", restored.Content)
	}
	if restored.Title != "Revision Test v1" {
		t.Errorf("after restore live title: got %q want 'Revision Test v1'", restored.Title)
	}

	// 7. List history again — should now have 3 revisions; the newest
	// captures the v3-pre-restore state ("Third draft content.").
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/"+created.ID+"/history", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ListMemoryArtifactRevisions(w, req)
	json.NewDecoder(w.Body).Decode(&histResp)
	if got := len(histResp.Revisions); got != 3 {
		t.Fatalf("expected 3 revisions after restore, got %d", got)
	}
	if histResp.Revisions[0].RevisionNumber != 3 {
		t.Errorf("newest revision after restore: got %d want 3", histResp.Revisions[0].RevisionNumber)
	}
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/memory/"+created.ID+"/history/3", nil)
	req = withURLParam(req, "id", created.ID)
	req = withURLParam(req, "revision", "3")
	testHandler.GetMemoryArtifactRevision(w, req)
	json.NewDecoder(w.Body).Decode(&revDetail)
	if revDetail.Content != "Third draft content." {
		t.Errorf("rev3 content (pre-restore snapshot): got %q want 'Third draft content.'", revDetail.Content)
	}
}

// silence unused-import warnings if `chi` is otherwise unreferenced when
// the new tests are skipped on a no-DB run. (`chi` is used for URL param
// helpers in the existing tests; this is purely belt-and-suspenders.)
var _ = chi.URLParam

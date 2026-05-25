package handler

// CEREBRO-PATCH(issue-dependencies): handler tests for blocks/blocked-by/related (FIR-823).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// depTestIssue creates an issue in the handler test workspace and registers
// cleanup. Returns the decoded IssueResponse.
func depTestIssue(t *testing.T, title string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

func depPost(t *testing.T, handler http.HandlerFunc, issueID, path, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+path, map[string]any{"issue_id": target})
	req = withURLParam(req, "id", issueID)
	handler(w, req)
	return w
}

func depGetDependencies(t *testing.T, issueID string) IssueDependenciesResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/dependencies", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueDependencies(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueDependencies: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var deps IssueDependenciesResponse
	if err := json.NewDecoder(w.Body).Decode(&deps); err != nil {
		t.Fatalf("decode dependencies: %v", err)
	}
	return deps
}

func containsRef(refs []IssueDependencyRef, id string) bool {
	for _, r := range refs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// TestIssueDependencyBlocksRoundTrip covers the core blocks/blocked-by flow:
// creating a 'blocks' edge from A is visible as blocked_by on B, and removable.
func TestIssueDependencyBlocksRoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "Dep A blocks")
	b := depTestIssue(t, "Dep B blocked")

	// A blocks B
	w := depPost(t, testHandler.AddBlocks, a.ID, "/blocks", b.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("AddBlocks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var deps IssueDependenciesResponse
	json.NewDecoder(w.Body).Decode(&deps)
	if !containsRef(deps.Blocks, b.ID) {
		t.Fatalf("AddBlocks: expected A.blocks to contain B, got %+v", deps.Blocks)
	}

	// B sees A as a blocker.
	bDeps := depGetDependencies(t, b.ID)
	if !containsRef(bDeps.BlockedBy, a.ID) {
		t.Fatalf("expected B.blocked_by to contain A, got %+v", bDeps.BlockedBy)
	}

	// Idempotent: adding the same edge again still returns 200, no duplicate.
	w = depPost(t, testHandler.AddBlocks, a.ID, "/blocks", b.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("AddBlocks (repeat): expected 200, got %d", w.Code)
	}
	json.NewDecoder(w.Body).Decode(&deps)
	count := 0
	for _, r := range deps.Blocks {
		if r.ID == b.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one A→B blocks edge, got %d", count)
	}

	// Delete the edge.
	w = httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+a.ID+"/blocks/"+b.ID, nil)
	req = withURLParams(req, "id", a.ID, "otherId", b.ID)
	testHandler.DeleteBlocks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteBlocks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if deps := depGetDependencies(t, a.ID); containsRef(deps.Blocks, b.ID) {
		t.Fatalf("DeleteBlocks: A.blocks still contains B")
	}
}

// TestIssueDependencyBlockedByStoresReverseEdge verifies that "A blocked_by C"
// persists as a single canonical 'blocks' edge (C → A), not a separate row.
func TestIssueDependencyBlockedByStoresReverseEdge(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "Dep A blocked-by")
	c := depTestIssue(t, "Dep C blocker")

	w := depPost(t, testHandler.AddBlockedBy, a.ID, "/blocked-by", c.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("AddBlockedBy: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	aDeps := depGetDependencies(t, a.ID)
	if !containsRef(aDeps.BlockedBy, c.ID) {
		t.Fatalf("expected A.blocked_by to contain C, got %+v", aDeps.BlockedBy)
	}
	cDeps := depGetDependencies(t, c.ID)
	if !containsRef(cDeps.Blocks, a.ID) {
		t.Fatalf("expected C.blocks to contain A, got %+v", cDeps.Blocks)
	}
}

// TestIssueDependencyCycleRejected verifies that an edge closing a cycle is
// rejected with 409 rather than persisted.
func TestIssueDependencyCycleRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "Cycle A")
	b := depTestIssue(t, "Cycle B")

	if w := depPost(t, testHandler.AddBlocks, a.ID, "/blocks", b.ID); w.Code != http.StatusOK {
		t.Fatalf("AddBlocks A→B: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// B blocks A would create A→B→A.
	w := depPost(t, testHandler.AddBlocks, b.ID, "/blocks", a.ID)
	if w.Code != http.StatusConflict {
		t.Fatalf("AddBlocks B→A (cycle): expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIssueDependencySelfRejected verifies an issue cannot depend on itself.
func TestIssueDependencySelfRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "Self dep")
	w := depPost(t, testHandler.AddBlocks, a.ID, "/blocks", a.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddBlocks self: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIssueDependencyRelatedSymmetric verifies related links are symmetric and
// stored once.
func TestIssueDependencyRelatedSymmetric(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "Related A")
	b := depTestIssue(t, "Related B")

	if w := depPost(t, testHandler.AddRelated, a.ID, "/related", b.ID); w.Code != http.StatusOK {
		t.Fatalf("AddRelated: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if deps := depGetDependencies(t, a.ID); !containsRef(deps.Related, b.ID) {
		t.Fatalf("expected A.related to contain B, got %+v", deps.Related)
	}
	if deps := depGetDependencies(t, b.ID); !containsRef(deps.Related, a.ID) {
		t.Fatalf("expected B.related to contain A (symmetric), got %+v", deps.Related)
	}

	// Adding the reverse must not create a second row.
	if w := depPost(t, testHandler.AddRelated, b.ID, "/related", a.ID); w.Code != http.StatusOK {
		t.Fatalf("AddRelated reverse: expected 200, got %d", w.Code)
	}
	var rowCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue_dependency WHERE type='related' AND (issue_id=$1 OR depends_on_issue_id=$1) AND (issue_id=$2 OR depends_on_issue_id=$2)`,
		a.ID, b.ID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count related rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 canonical related row, got %d", rowCount)
	}
}

// TestIssueDependencyListIncludesBlockedBy verifies the list endpoint folds
// blocked_by into each row (the morning-overview dedup signal).
func TestIssueDependencyListIncludesBlockedBy(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	a := depTestIssue(t, "List blocked target")
	c := depTestIssue(t, "List blocker")
	if w := depPost(t, testHandler.AddBlockedBy, a.ID, "/blocked-by", c.ID); w.Code != http.StatusOK {
		t.Fatalf("AddBlockedBy: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&open_only=true", nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	for _, issue := range resp.Issues {
		if issue.ID == a.ID {
			if issue.BlockedBy == nil || !containsRef(*issue.BlockedBy, c.ID) {
				t.Fatalf("expected list row for A to carry blocked_by C, got %+v", issue.BlockedBy)
			}
			return
		}
	}
	t.Fatalf("issue A not found in list response")
}

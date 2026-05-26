package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// childrenBatchFixture creates two parents with a couple of children each,
// returning their ids so tests can assert the batched endpoint groups them
// correctly. Cleanup is registered so rows are removed on test failure.
type childrenBatchFixture struct {
	parentA   IssueResponse
	parentB   IssueResponse
	childrenA []IssueResponse
	childrenB []IssueResponse
}

func newChildrenBatchFixture(t *testing.T) childrenBatchFixture {
	t.Helper()

	mkIssue := func(title, status, parentID string) IssueResponse {
		w := httptest.NewRecorder()
		body := map[string]any{
			"title":  title + " " + time.Now().Format(time.RFC3339Nano),
			"status": status,
		}
		if parentID != "" {
			body["parent_issue_id"] = parentID
		}
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var out IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode %q: %v", title, err)
		}
		return out
	}

	parentA := mkIssue("children-batch parent A", "in_progress", "")
	parentB := mkIssue("children-batch parent B", "in_progress", "")
	a1 := mkIssue("children-batch a1", "todo", parentA.ID)
	a2 := mkIssue("children-batch a2", "done", parentA.ID)
	b1 := mkIssue("children-batch b1", "todo", parentB.ID)

	t.Cleanup(func() {
		ctx := context.Background()
		for _, id := range []string{a1.ID, a2.ID, b1.ID, parentA.ID, parentB.ID} {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})

	return childrenBatchFixture{
		parentA:   parentA,
		parentB:   parentB,
		childrenA: []IssueResponse{a1, a2},
		childrenB: []IssueResponse{b1},
	}
}

func decodeIssueBatch(t *testing.T, w *httptest.ResponseRecorder) []IssueResponse {
	t.Helper()
	var body struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Issues
}

func TestListChildrenByParents_ReturnsChildrenForBothParentsInOneCall(t *testing.T) {
	fx := newChildrenBatchFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+fx.parentA.ID+","+fx.parentB.ID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := decodeIssueBatch(t, w)
	if len(got) != 3 {
		t.Fatalf("expected 3 children, got %d", len(got))
	}

	wantParents := map[string]int{fx.parentA.ID: 2, fx.parentB.ID: 1}
	have := map[string]int{}
	for _, issue := range got {
		if issue.ParentIssueID == nil {
			t.Fatalf("child %q is missing parent_issue_id", issue.ID)
		}
		have[*issue.ParentIssueID]++
	}
	for parent, want := range wantParents {
		if have[parent] != want {
			t.Fatalf("parent %s: want %d children, got %d", parent, want, have[parent])
		}
	}
}

func TestListChildrenByParents_EmptyParentIdsReturnsEmptyList(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeIssueBatch(t, w); len(got) != 0 {
		t.Fatalf("expected 0 children, got %d", len(got))
	}
}

func TestListChildrenByParents_UnknownParentYieldsNoChildren(t *testing.T) {
	// A well-formed UUID that doesn't exist in the workspace must produce
	// an empty response, not an error — the client uses this endpoint
	// optimistically and tolerates stale parent ids.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids=00000000-0000-0000-0000-000000000000", nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeIssueBatch(t, w); len(got) != 0 {
		t.Fatalf("expected 0 children, got %d", len(got))
	}
}

func TestListChildrenByParents_RejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids=not-a-uuid", nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChildrenByParents_RejectsTooManyParents(t *testing.T) {
	// A caller passing more than the documented cap is rejected; the cap
	// prevents a single request from materializing the workspace's entire
	// issue tree.
	ids := make([]string, listChildrenByParentsLimit+1)
	for i := range ids {
		ids[i] = "00000000-0000-0000-0000-000000000000"
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+strings.Join(ids, ","), nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

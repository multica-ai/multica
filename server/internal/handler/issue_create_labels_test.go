package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// createTestIssueLabel creates an issue-scoped label in the test workspace via
// the public handler and registers cleanup. It returns the new label id.
func createTestIssueLabel(t *testing.T, name string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  name,
		"color": "#ef4444",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var created LabelResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, created.ID)
	})
	return created.ID
}

func countIssueLabel(t *testing.T, issueID, labelID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue_to_label WHERE issue_id = $1 AND label_id = $2`,
		issueID, labelID,
	).Scan(&n); err != nil {
		t.Fatalf("count issue_to_label: %v", err)
	}
	return n
}

func countIssuesWithTitle(t *testing.T, title string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, title,
	).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

// TestCreateIssueAttachesLabelsAtomically verifies that labels passed in the
// create request are written in the same transaction as the issue, so a
// created issue already carries its labels without a second round-trip.
func TestCreateIssueAttachesLabelsAtomically(t *testing.T) {
	labelA := createTestIssueLabel(t, "cl-a-"+uuid.NewString()[:8])
	labelB := createTestIssueLabel(t, "cl-b-"+uuid.NewString()[:8])

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "create-labels attaches",
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{labelA, labelB},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	if got := countIssueLabel(t, issue.ID, labelA); got != 1 {
		t.Errorf("label A: expected 1 attachment, got %d", got)
	}
	if got := countIssueLabel(t, issue.ID, labelB); got != 1 {
		t.Errorf("label B: expected 1 attachment, got %d", got)
	}
}

// TestCreateIssueDedupesDuplicateLabelIDs verifies that a repeated label id in
// the request attaches exactly once (ON CONFLICT DO NOTHING) and still 201s.
func TestCreateIssueDedupesDuplicateLabelIDs(t *testing.T) {
	label := createTestIssueLabel(t, "cl-dup-"+uuid.NewString()[:8])

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "create-labels dedupe",
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{label, label},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	if got := countIssueLabel(t, issue.ID, label); got != 1 {
		t.Errorf("expected label attached exactly once, got %d", got)
	}
}

// TestCreateIssueRejectsUnknownLabelWithoutCreating verifies that an unknown
// label id fails the whole create with 400 and leaves no issue behind — the
// atomicity guarantee the old post-create attach flow could not offer.
func TestCreateIssueRejectsUnknownLabelWithoutCreating(t *testing.T) {
	const title = "create-labels unknown rejected"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":     title,
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{uuid.NewString()},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := countIssuesWithTitle(t, title); got != 0 {
		t.Errorf("expected no issue created on invalid label, found %d", got)
	}
}

// TestCreateIssueRejectsNonIssueScopedLabel verifies that a label scoped to a
// non-issue resource (e.g. agent) cannot be attached to an issue at create,
// mirroring the resource_type guard baked into AttachLabelToIssue.
func TestCreateIssueRejectsNonIssueScopedLabel(t *testing.T) {
	var agentLabelID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue_label (workspace_id, resource_type, name, color)
		 VALUES ($1, 'agent', $2, '#000000') RETURNING id`,
		testWorkspaceID, "create-labels agent "+uuid.NewString(),
	).Scan(&agentLabelID); err != nil {
		t.Fatalf("insert agent label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, agentLabelID)
	})

	const title = "create-labels wrong scope rejected"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":     title,
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{agentLabelID},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := countIssuesWithTitle(t, title); got != 0 {
		t.Errorf("expected no issue created on wrong-scope label, found %d", got)
	}
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListIssuesIncludesLabels verifies L-PR#2: ListIssues enriches every
// issue with its attached labels via a single bulk query.
func TestListIssuesIncludesLabels(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)
	ctx := context.Background()

	// Seed two labels + two issues. Issue 1 gets both labels; issue 2 gets one.
	var label1ID, label2ID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue_label (workspace_id, name, color, creator_type, creator_id)
		 VALUES ($1, 'Urgent', 'red', 'member', $2) RETURNING id`,
		workspaceID, userID).Scan(&label1ID); err != nil {
		t.Fatalf("seed label 1: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue_label (workspace_id, name, color, creator_type, creator_id)
		 VALUES ($1, 'Backend', 'blue', 'member', $2) RETURNING id`,
		workspaceID, userID).Scan(&label2ID); err != nil {
		t.Fatalf("seed label 2: %v", err)
	}

	var issue1ID, issue2ID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, 'Issue A', 'todo', 'medium', 'member', $2, 1) RETURNING id`,
		workspaceID, userID).Scan(&issue1ID); err != nil {
		t.Fatalf("seed issue 1: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, 'Issue B', 'todo', 'medium', 'member', $2, 2) RETURNING id`,
		workspaceID, userID).Scan(&issue2ID); err != nil {
		t.Fatalf("seed issue 2: %v", err)
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue_to_label (issue_id, label_id, actor_type, actor_id)
		 VALUES ($1, $2, 'member', $3), ($1, $4, 'member', $3), ($5, $2, 'member', $3)`,
		issue1ID, label1ID, userID, label2ID, issue2ID); err != nil {
		t.Fatalf("seed joins: %v", err)
	}

	// Call ListIssues.
	w := httptest.NewRecorder()
	req := labelReq("GET",
		fmt.Sprintf("/api/issues?workspace_id=%s", workspaceID),
		userID, workspaceID, nil,
	)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listResp.Issues) < 2 {
		t.Fatalf("expected at least 2 issues, got %d", len(listResp.Issues))
	}

	// Find our issues and verify their labels.
	byID := make(map[string]IssueResponse, len(listResp.Issues))
	for _, is := range listResp.Issues {
		byID[is.ID] = is
	}
	a, ok := byID[issue1ID]
	if !ok {
		t.Fatal("issue A not in response")
	}
	b, ok := byID[issue2ID]
	if !ok {
		t.Fatal("issue B not in response")
	}

	if len(a.Labels) != 2 {
		t.Fatalf("issue A: expected 2 labels, got %d (%+v)", len(a.Labels), a.Labels)
	}
	if len(b.Labels) != 1 {
		t.Fatalf("issue B: expected 1 label, got %d", len(b.Labels))
	}
	// Issue B was seeded with label1 (Urgent).
	if b.Labels[0].Name != "Urgent" {
		t.Fatalf("issue B: expected 'Urgent' label, got %q", b.Labels[0].Name)
	}
}

// TestGetIssueIncludesLabels verifies that the single-issue GET returns labels.
func TestGetIssueIncludesLabels(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)
	ctx := context.Background()

	var labelID, issueID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue_label (workspace_id, name, color, creator_type, creator_id)
		 VALUES ($1, 'Polish', 'teal', 'member', $2) RETURNING id`,
		workspaceID, userID).Scan(&labelID); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, 'Check me', 'todo', 'medium', 'member', $2, 1) RETURNING id`,
		workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue_to_label (issue_id, label_id, actor_type, actor_id)
		 VALUES ($1, $2, 'member', $3)`,
		issueID, labelID, userID); err != nil {
		t.Fatalf("seed join: %v", err)
	}

	w := httptest.NewRecorder()
	req := labelReq("GET",
		fmt.Sprintf("/api/issues/%s", issueID),
		userID, workspaceID, nil,
		"id", issueID,
	)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Labels) != 1 {
		t.Fatalf("expected 1 label, got %d", len(resp.Labels))
	}
	if resp.Labels[0].Name != "Polish" {
		t.Fatalf("expected 'Polish', got %q", resp.Labels[0].Name)
	}
}

// TestListIssuesReturnsEmptyLabelsArray verifies that issues without labels
// return `labels: []` (never null) \u2014 important for frontend null-safety.
func TestListIssuesReturnsEmptyLabelsArray(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, 'Plain', 'todo', 'medium', 'member', $2, 1)`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := labelReq("GET",
		fmt.Sprintf("/api/issues?workspace_id=%s", workspaceID),
		userID, workspaceID, nil,
	)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}

	// Assert the raw JSON contains `"labels":[]` so the frontend never gets null.
	if !strings.Contains(w.Body.String(), `"labels":[]`) {
		t.Fatalf("expected response to include `\"labels\":[]`, got: %s", w.Body.String())
	}
}

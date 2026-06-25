package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestListIssues_ExcludesWorkflowOriginByDefault verifies that issues with
// origin_type='workflow' are excluded from the default issue list. The parent
// issue (no origin_type) must still appear; the workflow-origin child must not.
func TestListIssues_ExcludesWorkflowOriginByDefault(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Insert parent issue (no origin_type).
	parentID := insertIssueOriginFilterFixture(t, ctx, fmt.Sprintf("parent-%d", suffix), "", "")

	// Insert child issue with origin_type='workflow'.
	childID := insertIssueOriginFilterFixture(t, ctx, fmt.Sprintf("child-%d", suffix), "workflow", parentID)

	path := fmt.Sprintf("/api/issues?workspace_id=%s&limit=500", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	foundParent := false
	foundChild := false
	for _, iss := range resp.Issues {
		if iss.ID == parentID {
			foundParent = true
		}
		if iss.ID == childID {
			foundChild = true
		}
	}

	if !foundParent {
		t.Fatalf("default list must include parent issue %s, but it was missing", parentID)
	}
	if foundChild {
		t.Fatalf("default list must exclude workflow-origin child %s, but it was present", childID)
	}
}

// TestListIssues_IncludeWorkflowOrigin verifies that when
// include_workflow_origin=true is passed, issues with origin_type='workflow'
// are included in the response alongside regular issues.
func TestListIssues_IncludeWorkflowOrigin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Insert parent issue (no origin_type).
	parentID := insertIssueOriginFilterFixture(t, ctx, fmt.Sprintf("include-parent-%d", suffix), "", "")

	// Insert child issue with origin_type='workflow'.
	childID := insertIssueOriginFilterFixture(t, ctx, fmt.Sprintf("include-child-%d", suffix), "workflow", parentID)

	path := fmt.Sprintf("/api/issues?workspace_id=%s&include_workflow_origin=true&limit=500", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	foundParent := false
	foundChild := false
	for _, iss := range resp.Issues {
		if iss.ID == parentID {
			foundParent = true
		}
		if iss.ID == childID {
			foundChild = true
		}
	}

	if !foundParent {
		t.Fatalf("include_workflow_origin=true list must include parent issue %s, but it was missing", parentID)
	}
	if !foundChild {
		t.Fatalf("include_workflow_origin=true list must include workflow-origin child %s, but it was missing", childID)
	}
}

// insertIssueOriginFilterFixture creates an issue in the handler test workspace
// and returns its ID. If originType is non-empty, the issue is stamped with
// that origin_type. If parentID is non-empty, parent_issue_id is set.
// The issue is registered for cleanup via t.Cleanup.
func insertIssueOriginFilterFixture(t *testing.T, ctx context.Context, title, originType, parentID string) string {
	t.Helper()

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE multica_workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM multica_issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var id string
	var parentArg *string
	if parentID != "" {
		parentArg = &parentID
	}
	var originArg *string
	if originType != "" {
		originArg = &originType
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, origin_type, parent_issue_id)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5, $6) RETURNING id
	`, testWorkspaceID, title, testUserID, number, originArg, parentArg).Scan(&id); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM multica_issue WHERE id = $1`, id)
	})

	return id
}

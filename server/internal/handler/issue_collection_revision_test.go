package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestIssueCollectionProjectionsIncludePositiveRevision(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Revision projection %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var issueNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&issueNumber); err != nil {
		t.Fatalf("reserve issue number: %v", err)
	}

	title := fmt.Sprintf("revision-projection-%d", suffix)
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id, revision
		)
		VALUES ($1, $2, 'in_review', 'none', 'member', $3, 1, $4, $5, 7)
		RETURNING id
	`, testWorkspaceID, title, testUserID, issueNumber, projectID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	groupKey := "status:in_review"
	tableRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(tableRecorder, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope: issueTableScope{Kind: "project", ProjectID: projectID},
			Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:     issueTableGroupSpec{Kind: "status"},
		GroupKey:  &groupKey,
		Hierarchy: issueTableHierarchyRequest{Enabled: false},
		Page:      issueTablePageRequest{Limit: 10},
	}))
	if tableRecorder.Code != http.StatusOK {
		t.Fatalf("table rows status = %d: %s", tableRecorder.Code, tableRecorder.Body.String())
	}
	var tableResponse issueTableRowsResponse
	if err := json.NewDecoder(tableRecorder.Body).Decode(&tableResponse); err != nil {
		t.Fatalf("decode table rows: %v", err)
	}
	if len(tableResponse.Rows) != 1 || tableResponse.Rows[0].Issue.ID != issueID {
		t.Fatalf("table rows = %+v, want issue %s", tableResponse.Rows, issueID)
	}
	if tableResponse.Rows[0].Issue.Revision != 7 {
		t.Fatalf("table row revision = %d, want 7", tableResponse.Rows[0].Issue.Revision)
	}

	listRecorder := httptest.NewRecorder()
	testHandler.ListIssues(listRecorder, newRequest(http.MethodGet, "/api/issues?project_id="+url.QueryEscape(projectID), nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list issues status = %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatalf("decode issue list: %v", err)
	}
	if len(listResponse.Issues) != 1 || listResponse.Issues[0].ID != issueID || listResponse.Issues[0].Revision != 7 {
		t.Fatalf("list projection = %+v, want issue %s at revision 7", listResponse.Issues, issueID)
	}

	searchRecorder := httptest.NewRecorder()
	testHandler.SearchIssues(searchRecorder, newRequest(http.MethodGet, "/api/issues/search?q="+url.QueryEscape(title), nil))
	if searchRecorder.Code != http.StatusOK {
		t.Fatalf("search issues status = %d: %s", searchRecorder.Code, searchRecorder.Body.String())
	}
	var searchResponse struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(searchRecorder.Body).Decode(&searchResponse); err != nil {
		t.Fatalf("decode issue search: %v", err)
	}
	if len(searchResponse.Issues) != 1 || searchResponse.Issues[0].ID != issueID || searchResponse.Issues[0].Revision != 7 {
		t.Fatalf("search projection = %+v, want issue %s at revision 7", searchResponse.Issues, issueID)
	}
}

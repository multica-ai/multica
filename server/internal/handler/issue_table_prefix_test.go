package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// issueTableRowsForSearch runs the table view's row endpoint over the whole
// workspace, narrowed by a search token, and returns the rows by issue id.
func issueTableRowsForSearch(t *testing.T, search string) map[string]issueTableRowResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:  issueTableScope{Kind: "workspace"},
			Search: search,
			Sort:   issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 50},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("table rows status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response issueTableRowsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode table rows: %v", err)
	}
	byID := make(map[string]issueTableRowResponse, len(response.Rows))
	for _, row := range response.Rows {
		byID[row.Issue.ID] = row
	}
	return byID
}

// TestIssueTableRowsUseProjectIdentifierPrefix covers the task list's row
// endpoint: a project that owns a prefix must render its own identifier in the
// table/list/board rows, while issues outside that project keep the workspace
// prefix.
func TestIssueTableRowsUseProjectIdentifierPrefix(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	token := "tablerow" + t.Name()
	projectID := createProjectForTest(t, "Table prefix project "+t.Name(), "PTBL")

	projectIssue := createIssueForTest(t, map[string]any{
		"title":      token + " project scoped",
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
	})
	if want := "PTBL-" + strconv.Itoa(int(projectIssue.Number)); projectIssue.Identifier != want {
		t.Fatalf("create response identifier = %q, want %q", projectIssue.Identifier, want)
	}
	workspaceIssue := createIssueForTest(t, map[string]any{
		"title":    token + " workspace scoped",
		"status":   "todo",
		"priority": "medium",
	})

	byID := issueTableRowsForSearch(t, token)
	projectRow, ok := byID[projectIssue.ID]
	if !ok {
		t.Fatalf("project issue %s missing from table rows: %#v", projectIssue.ID, byID)
	}
	if want := "PTBL-" + strconv.Itoa(int(projectIssue.Number)); projectRow.Issue.Identifier != want {
		t.Fatalf("table row identifier = %q, want %q", projectRow.Issue.Identifier, want)
	}
	workspaceRow, ok := byID[workspaceIssue.ID]
	if !ok {
		t.Fatalf("workspace issue %s missing from table rows: %#v", workspaceIssue.ID, byID)
	}
	if want := "SHAR-" + strconv.Itoa(int(workspaceIssue.Number)); workspaceRow.Issue.Identifier != want {
		t.Fatalf("workspace row identifier = %q, want %q — prefix-less issues must keep the workspace prefix", workspaceRow.Issue.Identifier, want)
	}
}

// TestIssueTableParentGroupUsesProjectIdentifierPrefix covers the grouped views
// (board / swimlane / parent grouping): the parent header carries an
// identifier, which must resolve in the parent's own numbering scope.
func TestIssueTableParentGroupUsesProjectIdentifierPrefix(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	projectID := createProjectForTest(t, "Parent prefix project "+t.Name(), "PTGR")

	parent := createIssueForTest(t, map[string]any{
		"title":      "parent under project prefix " + t.Name(),
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
	})
	createIssueForTest(t, map[string]any{
		"title":           "child under project prefix " + t.Name(),
		"status":          "todo",
		"priority":        "medium",
		"project_id":      projectID,
		"parent_issue_id": parent.ID,
	})

	recorder := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(recorder, newRequest("POST", "/api/issues/table/groups", issueTableGroupsRequest{
		Query: issueTableQuerySpec{
			Scope: issueTableScope{Kind: "project", ProjectID: projectID},
			Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "parent"},
		Page:  issueTablePageRequest{Limit: 10},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("table groups status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response issueTableGroupsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode table groups: %v", err)
	}
	var found *issueTableParentRef
	for _, group := range response.Groups {
		if group.Value.Parent != nil && group.Value.Parent.ID == parent.ID {
			found = group.Value.Parent
			break
		}
	}
	if found == nil {
		t.Fatalf("parent group %s missing from groups: %#v", parent.ID, response.Groups)
	}
	if want := "PTGR-" + strconv.Itoa(int(parent.Number)); found.Identifier != want {
		t.Fatalf("parent group identifier = %q, want %q", found.Identifier, want)
	}
}

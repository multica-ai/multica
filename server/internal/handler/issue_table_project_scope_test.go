package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueTableProjectSelection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	inherited := fx.Project(t, "Inherited project")
	custom := fx.Project(t, "Custom project")
	workspaceWorkflow := fx.Insert(t, "issue_workflow", testutil.Cols{"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": inherited, "name": "Shared workflow"})
	customWorkflow := fx.Insert(t, "issue_workflow", testutil.Cols{"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": custom, "name": "Custom workflow"})
	node := func(workflow, name string, position int) string {
		return fx.Insert(t, "issue_workflow_status", testutil.Cols{"workspace_id": testWorkspaceID, "workflow_id": workflow, "name": name, "phase": "started", "color": "#123456", "position": position})
	}
	sharedNode := node(workspaceWorkflow, "In progress", 0)
	build := node(customWorkflow, "Build", 0)
	review := node(customWorkflow, "Review", 1)
	seed := func(title, workflow, status string, project any, assigned bool) string {
		cols := testutil.Cols{"project_id": project, "workflow_id": workflow, "workflow_status_id": status, "status": "in_progress"}
		if assigned {
			cols["assignee_id"] = testUserID
			cols["assignee_type"] = "member"
		}
		return fx.Issue(t, title, cols)
	}
	unprojected := seed("Scope no project", workspaceWorkflow, sharedNode, nil, true)
	inheritedIssue := seed("Scope inherited", workspaceWorkflow, sharedNode, inherited, true)
	buildIssue := seed("Scope build", customWorkflow, build, custom, true)
	reviewIssue := seed("Scope review", customWorkflow, review, custom, false)
	rows := func(query issueTableQuerySpec, group issueTableGroupSpec, key *string) issueTableRowsResponse {
		t.Helper()
		var out issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{Query: query, Group: group, GroupKey: key, Page: issueTablePageRequest{Limit: 100}})).Want(http.StatusOK).JSON(&out)
		return out
	}
	assertIDs := func(out issueTableRowsResponse, want ...string) {
		t.Helper()
		ids := map[string]bool{}
		for _, row := range out.Rows {
			ids[row.Issue.ID] = true
		}
		if len(ids) != len(want) {
			t.Fatalf("rows = %v, want %v", ids, want)
		}
		for _, id := range want {
			if !ids[id] {
				t.Fatalf("missing %s: %v", id, ids)
			}
		}
	}
	query := issueTableQuerySpec{Scope: issueTableScope{Kind: "workspace", WorkflowID: workspaceWorkflow}, Search: "Scope", Sort: issueTableSortRequest{Field: "position", Direction: "asc"}}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), unprojected, inheritedIssue)
	query.Scope = issueTableScope{Kind: "workspace", ProjectID: custom}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), buildIssue, reviewIssue)
	query.Filters.ProjectIDs = []string{inherited}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil))
	query.Filters.ProjectIDs = nil
	query.Scope.Kind = "my"
	query.Scope.Relation = "assigned"
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), buildIssue)
	query.Scope = issueTableScope{Kind: "workspace", ProjectID: custom}
	query.Filters.WorkflowStatusIDs = []string{review}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), reviewIssue)
	var facets issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets, newRequest(http.MethodPost, "/api/issues/table/facets", issueTableFacetsRequest{Query: query, Facets: []issueTableFacetSpec{{Kind: "workflow_status"}}})).Want(http.StatusOK).JSON(&facets)
	if facets.Total != 1 || len(facets.Facets) != 1 || len(facets.Facets[0].Values) != 2 {
		t.Fatalf("disjunctive status counts must remain inside project: %#v", facets)
	}
	query.Filters.WorkflowStatusIDs = []string{}
	var emptyRows issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", map[string]any{
		"query": map[string]any{"scope": query.Scope, "filters": map[string]any{"workflow_status_ids": []string{}}, "sort": query.Sort},
		"group": issueTableGroupSpec{Kind: "none"}, "page": issueTablePageRequest{Limit: 100},
	})).Want(http.StatusOK).JSON(&emptyRows)
	assertIDs(emptyRows)
	emptyFingerprint, _ := canonicalIssueTableFingerprint(testWorkspaceID, query)
	query.Filters.WorkflowStatusIDs = nil
	allFingerprint, _ := canonicalIssueTableFingerprint(testWorkspaceID, query)
	if emptyFingerprint == allFingerprint {
		t.Fatal("empty and absent node filters must have distinct cursors")
	}
	// Compound cells must keep two nodes in the same phase independent, including row cursors.
	group := issueTableGroupSpec{Kind: "compound", Primary: "project", Secondary: "workflow_status"}
	var groups issueTableGroupsResponse
	testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{Query: query, Group: group, Page: issueTablePageRequest{Limit: 100}})).Want(http.StatusOK).JSON(&groups)
	if len(groups.Groups) != 1 || len(groups.Groups[0].SecondaryGroups) != 2 {
		t.Fatalf("expected two node cells: %#v", groups)
	}
	for _, cell := range groups.Groups[0].SecondaryGroups {
		if cell.Value.WorkflowStatusID == nil || cell.Count != 1 {
			t.Fatalf("invalid cell: %#v", cell)
		}
		want := buildIssue
		if *cell.Value.WorkflowStatusID == review {
			want = reviewIssue
		}
		assertIDs(rows(query, group, &cell.Key), want)
	}
	query.Filters.WorkflowStatusIDs = []string{build}
	query.Filters.Statuses = []string{"in_progress"}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), buildIssue, reviewIssue)
	query.Filters.WorkflowStatusIDs = nil
	query.Filters.Statuses = nil
	// A legacy row must stay visible and must not cause NULL aggregation errors.
	legacyIssue := fx.Issue(t, "Scope legacy", testutil.Cols{"project_id": custom, "status": "todo"})
	testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{Query: query, Group: group, Page: issueTablePageRequest{Limit: 100}})).Want(http.StatusOK).JSON(&groups)
	found := false
	for _, cell := range groups.Groups[0].SecondaryGroups {
		if cell.Value.WorkflowStatusID == nil {
			assertIDs(rows(query, group, &cell.Key), legacyIssue)
			found = true
		}
	}
	if !found {
		t.Fatal("legacy row disappeared from workflow cells")
	}
	for _, badScope := range []issueTableScope{{Kind: "workspace", ProjectID: "bad"}, {Kind: "my", Relation: "assigned", WorkflowID: "bad"}} {
		query.Scope = badScope
		testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{Query: query, Group: issueTableGroupSpec{Kind: "none"}, Page: issueTablePageRequest{Limit: 100}})).Want(http.StatusBadRequest)
	}
}

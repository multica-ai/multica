package handler

import (
	"context"
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
	// Status sorting must distinguish nodes in the same lifecycle phase, and
	// its cursor must continue from the concrete node rank in either direction.
	for _, direction := range []string{"asc", "desc"} {
		sortedQuery := query
		sortedQuery.Sort = issueTableSortRequest{Field: "status", Direction: direction}
		want := []string{buildIssue, reviewIssue}
		if direction == "desc" {
			want = []string{reviewIssue, buildIssue}
		}
		var cursor *string
		for _, id := range want {
			var page issueTableRowsResponse
			testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: sortedQuery, Group: issueTableGroupSpec{Kind: "none"}, Page: issueTablePageRequest{Limit: 1, Cursor: cursor},
			})).Want(http.StatusOK).JSON(&page)
			assertIDs(page, id)
			cursor = page.NextCursor
		}
		if cursor != nil {
			t.Fatal("unexpected extra status-sort page")
		}
	}
	// A selected node remains labelled when another filter removes every row.
	noResults := query
	noResults.Search = "does-not-match-any-issue"
	noResults.Filters.WorkflowStatusIDs = []string{review}
	var emptyFacets issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets, newRequest(http.MethodPost, "/api/issues/table/facets", issueTableFacetsRequest{
		Query: noResults, Facets: []issueTableFacetSpec{{Kind: "workflow_status"}},
	})).Want(http.StatusOK).JSON(&emptyFacets)
	if len(emptyFacets.Facets) != 1 || len(emptyFacets.Facets[0].Values) != 1 {
		t.Fatalf("missing selected zero-count node: %#v", emptyFacets)
	}
	selected := emptyFacets.Facets[0].Values[0]
	if selected.Key != review || selected.Count != 0 || selected.StatusNode == nil || selected.StatusNode.Name != "Review" {
		t.Fatalf("lost selected node metadata: %#v", selected)
	}
	// Assignee/project swimlanes need empty targets from the represented workflow.
	emptyNode := node(customWorkflow, "QA", 2)
	var targets issueTableGroupsResponse
	testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query, Group: group, Page: issueTablePageRequest{Limit: 100},
	})).Want(http.StatusOK).JSON(&targets)
	if len(targets.Groups) != 1 || len(targets.Groups[0].SecondaryGroups) != 3 {
		t.Fatalf("missing empty workflow target: %#v", targets)
	}
	last := targets.Groups[0].SecondaryGroups[2]
	if last.Value.WorkflowStatusID == nil || *last.Value.WorkflowStatusID != emptyNode || last.Count != 0 {
		t.Fatalf("wrong empty target: %#v", last)
	}
	query.Filters.WorkflowStatusIDs = []string{build}
	query.Filters.Statuses = []string{"in_progress"}
	assertIDs(rows(query, issueTableGroupSpec{Kind: "none"}, nil), buildIssue, reviewIssue)
	query.Filters.WorkflowStatusIDs = nil
	query.Filters.Statuses = nil
	// A legacy row must stay visible and must not cause NULL aggregation errors.
	legacyIssue := fx.Issue(t, "Scope legacy", testutil.Cols{"project_id": custom, "status": "todo"})
	groups = issueTableGroupsResponse{} // Decode optional fields into a fresh response.
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

func TestIssueTableDefaultWorkflowComesFirstAcrossPages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	var previous *string
	if err := testPool.QueryRow(ctx, "SELECT default_issue_workflow_id::text FROM workspace WHERE id=$1", testWorkspaceID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	workflows := []string{}
	for _, name := range []string{"AAA secondary", "ZZZ default"} {
		project := fx.Project(t, name)
		workflow := fx.Insert(t, "issue_workflow", testutil.Cols{"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": project, "name": name})
		node := fx.Insert(t, "issue_workflow_status", testutil.Cols{"workspace_id": testWorkspaceID, "workflow_id": workflow, "name": "Build", "phase": "started", "color": "#123456", "position": 0})
		fx.Issue(t, "Default lane ordering fixture", testutil.Cols{"project_id": project, "workflow_id": workflow, "workflow_status_id": node, "status": "in_progress"})
		workflows = append(workflows, workflow)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "UPDATE workspace SET default_issue_workflow_id=$2 WHERE id=$1", testWorkspaceID, previous)
	})
	if _, err := testPool.Exec(ctx, "UPDATE workspace SET default_issue_workflow_id=$2 WHERE id=$1", testWorkspaceID, workflows[1]); err != nil {
		t.Fatal(err)
	}
	query := issueTableQuerySpec{Scope: issueTableScope{Kind: "workspace"}, Search: "Default lane ordering fixture", Sort: issueTableSortRequest{Field: "position", Direction: "asc"}}
	var cursor *string
	for index, workflow := range []string{workflows[1], workflows[0]} {
		var page issueTableGroupsResponse
		testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
			Query: query, Group: issueTableGroupSpec{Kind: "compound", Primary: "workflow", Secondary: "workflow_status"}, Page: issueTablePageRequest{Limit: 1, Cursor: cursor},
		})).Want(http.StatusOK).JSON(&page)
		if len(page.Groups) != 1 || page.Groups[0].Value.WorkflowID == nil || *page.Groups[0].Value.WorkflowID != workflow || page.Groups[0].Value.IsDefault != (index == 0) {
			t.Fatalf("default workflow was not first independently of name: %#v", page)
		}
		cursor = page.NextCursor
	}
	if cursor != nil {
		t.Fatal("unexpected third workflow page")
	}
}

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

// Two project workflows are allowed to use the same display name. The table
// contract must keep them separate by stable node id; grouping on the label
// would mix unrelated workflows and make a drag target ambiguous.
func TestIssueTableWorkflowStatusGroupsIsolateSameNameNodes(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	projectIDs := make([]string, 2)
	workflowIDs := make([]string, 2)
	statusIDs := make([]string, 2)

	for i := range 2 {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title)
			VALUES ($1, $2)
			RETURNING id::text
		`, testWorkspaceID, fmt.Sprintf("Workflow grouping %d/%d", suffix, i)).Scan(&projectIDs[i]); err != nil {
			t.Fatalf("create project %d: %v", i, err)
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_workflow (workspace_id, scope_type, scope_id, name)
			VALUES ($1, 'project', $2, $3)
			RETURNING id::text
		`, testWorkspaceID, projectIDs[i], fmt.Sprintf("Workflow %d", i)).Scan(&workflowIDs[i]); err != nil {
			t.Fatalf("create workflow %d: %v", i, err)
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_workflow_status (
				workspace_id, workflow_id, legacy_status_key, name, color, position, phase
			)
			VALUES ($1, $2, 'todo', 'Implementation', $3, $4, 'unstarted')
			RETURNING id::text
		`, testWorkspaceID, workflowIDs[i], []string{"#2563eb", "#7c3aed"}[i], i).Scan(&statusIDs[i]); err != nil {
			t.Fatalf("create workflow status %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = ANY($1::uuid[])`, projectIDs)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workflow_id = ANY($1::uuid[])`, workflowIDs)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = ANY($1::uuid[])`, workflowIDs)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = ANY($1::uuid[])`, projectIDs)
	})

	var firstNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 2
		WHERE id = $1
		RETURNING issue_counter - 1
	`, testWorkspaceID).Scan(&firstNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}
	for i := range 2 {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, project_id, workflow_id, workflow_status_id
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, $5, $6, $7, $8)
		`, testWorkspaceID, fmt.Sprintf("same-name-node-%d", i), testUserID, i, firstNumber+i, projectIDs[i], workflowIDs[i], statusIDs[i]); err != nil {
			t.Fatalf("create issue %d: %v", i, err)
		}
	}

	query := issueTableQuerySpec{
		Scope:   issueTableScope{Kind: "workspace"},
		Filters: issueTableFiltersRequest{ProjectIDs: projectIDs},
		Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	w := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(w, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query,
		Group: issueTableGroupSpec{Kind: "workflow_status"},
		Page:  issueTablePageRequest{Limit: 100},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("groups status = %d: %s", w.Code, w.Body.String())
	}
	var groups issueTableGroupsResponse
	if err := json.NewDecoder(w.Body).Decode(&groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(groups.Groups) != 2 || groups.Total != 2 {
		t.Fatalf("groups = %#v, want two isolated nodes and total 2", groups)
	}

	seen := map[string]bool{}
	for _, group := range groups.Groups {
		if group.Value.Kind != "workflow_status" || group.Value.Name != "Implementation" || group.Value.WorkflowStatusID == nil {
			t.Fatalf("unexpected workflow descriptor: %#v", group)
		}
		statusID := *group.Value.WorkflowStatusID
		seen[statusID] = true
		if group.Key != "workflow_status:"+statusID || group.Count != 1 {
			t.Fatalf("descriptor identity/count mismatch: %#v", group)
		}

		rowsRecorder := httptest.NewRecorder()
		key := group.Key
		testHandler.ListIssueTableRows(rowsRecorder, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query:    query,
			Group:    issueTableGroupSpec{Kind: "workflow_status"},
			GroupKey: &key,
			Page:     issueTablePageRequest{Limit: 50},
		}))
		if rowsRecorder.Code != http.StatusOK {
			t.Fatalf("rows status = %d: %s", rowsRecorder.Code, rowsRecorder.Body.String())
		}
		var rows issueTableRowsResponse
		if err := json.NewDecoder(rowsRecorder.Body).Decode(&rows); err != nil {
			t.Fatalf("decode rows: %v", err)
		}
		if len(rows.Rows) != 1 || rows.Rows[0].Issue.WorkflowStatusID == nil || *rows.Rows[0].Issue.WorkflowStatusID != statusID {
			t.Fatalf("rows crossed status-node boundary: %#v", rows.Rows)
		}
	}
	for _, statusID := range statusIDs {
		if !seen[statusID] {
			t.Fatalf("missing status node %s from descriptors: %#v", statusID, groups.Groups)
		}
	}
	// A workflow lane includes its active empty targets but never another
	// workflow's nodes, even when both workflows use the same display labels.
	emptyIDs := make([]string, 2)
	for i := range 2 {
		if err := testPool.QueryRow(ctx, `INSERT INTO issue_workflow_status
            (workspace_id, workflow_id, name, color, position, phase)
            VALUES ($1, $2, 'Review', '#2563eb', 10, 'started') RETURNING id::text`,
			testWorkspaceID, workflowIDs[i]).Scan(&emptyIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	compound := issueTableGroupSpec{Kind: "compound", Primary: "workflow", Secondary: "workflow_status"}
	var cursor *string
	visited := map[string]bool{}
	for range 2 {
		recorder := httptest.NewRecorder()
		testHandler.ListIssueTableGroups(recorder, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
			Query: query, Group: compound, Page: issueTablePageRequest{Limit: 1, Cursor: cursor},
		}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("lanes status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var page issueTableGroupsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Groups) != 1 {
			t.Fatalf("incorrect paged lanes: %#v", page)
		}
		lane := page.Groups[0]
		if lane.Value.WorkflowID == nil || lane.Count != 1 || len(lane.SecondaryGroups) != 2 {
			t.Fatalf("lane omitted empty target or mixed workflows: %#v", lane)
		}
		if visited[lane.Key] {
			t.Fatalf("lane repeated across pages: %s", lane.Key)
		}
		visited[lane.Key] = true
		for index, cell := range lane.SecondaryGroups {
			if cell.Value.WorkflowID == nil || *cell.Value.WorkflowID != *lane.Value.WorkflowID {
				t.Fatalf("column crossed workflow boundary: %#v", cell)
			}
			if cell.Count != int64(1-index) {
				t.Fatalf("column order/count mismatch: %#v", cell)
			}
			rowsRecorder := httptest.NewRecorder()
			testHandler.ListIssueTableRows(rowsRecorder, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: query, Group: compound, GroupKey: &cell.Key, Page: issueTablePageRequest{Limit: 50},
			}))
			if rowsRecorder.Code != http.StatusOK {
				t.Fatalf("cell rows: %d %s", rowsRecorder.Code, rowsRecorder.Body.String())
			}
			var rows issueTableRowsResponse
			if err := json.NewDecoder(rowsRecorder.Body).Decode(&rows); err != nil {
				t.Fatal(err)
			}
			if len(rows.Rows) != int(cell.Count) {
				t.Fatalf("empty column or row mismatch: %#v", rows)
			}
			for _, row := range rows.Rows {
				if row.Issue.WorkflowID == nil || *row.Issue.WorkflowID != *lane.Value.WorkflowID {
					t.Fatal("row crossed workflow")
				}
			}
		}
		cursor = page.NextCursor
	}
	if cursor != nil || len(visited) != 2 {
		t.Fatal("workflow pagination did not terminate")
	}

	query.Filters.WorkflowStatusIDs = []string{statusIDs[0]}
	facetRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableFacets(facetRecorder, newRequest(http.MethodPost, "/api/issues/table/facets", issueTableFacetsRequest{
		Query: query, Facets: []issueTableFacetSpec{{Kind: "workflow_status"}},
	}))
	if facetRecorder.Code != http.StatusOK {
		t.Fatalf("workflow facets: %d %s", facetRecorder.Code, facetRecorder.Body.String())
	}
	var facets issueTableFacetsResponse
	if err := json.NewDecoder(facetRecorder.Body).Decode(&facets); err != nil {
		t.Fatal(err)
	}
	if len(facets.Facets) != 1 || len(facets.Facets[0].Values) != 2 {
		t.Fatalf("status filter lost disjunctive choices: %#v", facets)
	}
	for _, value := range facets.Facets[0].Values {
		if value.StatusNode == nil || value.StatusNode.WorkflowID == nil || value.StatusNode.WorkflowName == "" || value.StatusNode.Name != "Implementation" {
			t.Fatalf("facet missing qualified workflow label: %#v", value)
		}
	}
	query.Filters.WorkflowStatusIDs = nil

	// Project membership can change independently of an issue's pinned workflow.
	// Filtering that project must still render the original workflow lane.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id=$1 WHERE workflow_id=$2 AND workspace_id=$3`, projectIDs[1], workflowIDs[0], testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	query.Filters.ProjectIDs = []string{projectIDs[1]}
	query.Filters.WorkflowStatusIDs = []string{statusIDs[0]}
	filteredRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(filteredRecorder, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query, Group: compound, Page: issueTablePageRequest{Limit: 50},
	}))
	if filteredRecorder.Code != http.StatusOK {
		t.Fatalf("filtered lanes: %d %s", filteredRecorder.Code, filteredRecorder.Body.String())
	}
	var filtered issueTableGroupsResponse
	if err := json.NewDecoder(filteredRecorder.Body).Decode(&filtered); err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Groups) != 1 || *filtered.Groups[0].Value.WorkflowID != workflowIDs[0] {
		t.Fatalf("project filter resolved current default instead of pinned workflow: %#v", filtered)
	}

}

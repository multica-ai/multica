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

// Two project lifecycles are allowed to use the same display name. The table
// contract must keep them separate by stable node id; grouping on the label
// would mix unrelated workflows and make a drag target ambiguous.
func TestIssueTableLifecycleStatusGroupsIsolateSameNameNodes(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	projectIDs := make([]string, 2)
	lifecycleIDs := make([]string, 2)
	statusIDs := make([]string, 2)

	for i := range 2 {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title)
			VALUES ($1, $2)
			RETURNING id::text
		`, testWorkspaceID, fmt.Sprintf("Lifecycle grouping %d/%d", suffix, i)).Scan(&projectIDs[i]); err != nil {
			t.Fatalf("create project %d: %v", i, err)
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_lifecycle (workspace_id, scope_type, scope_id, name)
			VALUES ($1, 'project', $2, $3)
			RETURNING id::text
		`, testWorkspaceID, projectIDs[i], fmt.Sprintf("Workflow %d", i)).Scan(&lifecycleIDs[i]); err != nil {
			t.Fatalf("create lifecycle %d: %v", i, err)
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_lifecycle_status (
				workspace_id, lifecycle_id, legacy_status_key, name, color, position, phase
			)
			VALUES ($1, $2, 'todo', 'Implementation', $3, $4, 'unstarted')
			RETURNING id::text
		`, testWorkspaceID, lifecycleIDs[i], []string{"#2563eb", "#7c3aed"}[i], i).Scan(&statusIDs[i]); err != nil {
			t.Fatalf("create lifecycle status %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = ANY($1::uuid[])`, projectIDs)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle_status WHERE lifecycle_id = ANY($1::uuid[])`, lifecycleIDs)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle WHERE id = ANY($1::uuid[])`, lifecycleIDs)
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
				position, number, project_id, lifecycle_id, lifecycle_status_id
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, $5, $6, $7, $8)
		`, testWorkspaceID, fmt.Sprintf("same-name-node-%d", i), testUserID, i, firstNumber+i, projectIDs[i], lifecycleIDs[i], statusIDs[i]); err != nil {
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
		Group: issueTableGroupSpec{Kind: "lifecycle_status"},
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
		if group.Value.Kind != "lifecycle_status" || group.Value.Name != "Implementation" || group.Value.LifecycleStatusID == nil {
			t.Fatalf("unexpected lifecycle descriptor: %#v", group)
		}
		statusID := *group.Value.LifecycleStatusID
		seen[statusID] = true
		if group.Key != "lifecycle_status:"+statusID || group.Count != 1 {
			t.Fatalf("descriptor identity/count mismatch: %#v", group)
		}

		rowsRecorder := httptest.NewRecorder()
		key := group.Key
		testHandler.ListIssueTableRows(rowsRecorder, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query:    query,
			Group:    issueTableGroupSpec{Kind: "lifecycle_status"},
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
		if len(rows.Rows) != 1 || rows.Rows[0].Issue.LifecycleStatusID == nil || *rows.Rows[0].Issue.LifecycleStatusID != statusID {
			t.Fatalf("rows crossed status-node boundary: %#v", rows.Rows)
		}
	}
	for _, statusID := range statusIDs {
		if !seen[statusID] {
			t.Fatalf("missing status node %s from descriptors: %#v", statusID, groups.Groups)
		}
	}
}

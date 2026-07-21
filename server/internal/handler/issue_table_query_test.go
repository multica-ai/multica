package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type issueTableEnrichmentFailTxStarter struct {
	inner      txStarter
	labelCalls *int
}

func (s issueTableEnrichmentFailTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &issueTableEnrichmentFailTx{Tx: tx, labelCalls: s.labelCalls}, nil
}

type issueTableEnrichmentFailTx struct {
	pgx.Tx
	labelCalls *int
}

func (tx *issueTableEnrichmentFailTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "ListLabelsForIssues") {
		*tx.labelCalls = *tx.labelCalls + 1
		// A real PostgreSQL statement error poisons the transaction until
		// rollback. Before enrichment moved after Commit, this turned the
		// otherwise successful row window into a 500.
		_, err := tx.Tx.Exec(ctx, "SELECT * FROM issue_table_missing_enrichment_relation")
		return nil, err
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func TestCanonicalIssueTableFingerprintNormalizesSetLikeArrays(t *testing.T) {
	left := issueTableQuerySpec{
		Scope: issueTableScope{Kind: "workspace", AssigneeTypes: []string{"agent", "member", "agent"}},
		Filters: issueTableFiltersRequest{
			Statuses:   []string{"todo", "backlog", "todo"},
			ProjectIDs: []string{"b", "a"},
		},
		Sort: issueTableSortRequest{Field: "title", Direction: "asc"},
	}
	right := issueTableQuerySpec{
		Scope: issueTableScope{Kind: "workspace", AssigneeTypes: []string{"member", "agent"}},
		Filters: issueTableFiltersRequest{
			Statuses:   []string{"backlog", "todo"},
			ProjectIDs: []string{"a", "b"},
		},
		Sort: issueTableSortRequest{Field: "title", Direction: "asc"},
	}
	leftFingerprint, err := canonicalIssueTableFingerprint("workspace-1", left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := canonicalIssueTableFingerprint("workspace-1", right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("equivalent table queries produced different fingerprints: %s != %s", leftFingerprint, rightFingerprint)
	}
}

func TestCanonicalIssueTableFingerprintBindsWorkspace(t *testing.T) {
	spec := issueTableQuerySpec{
		Scope: issueTableScope{Kind: "workspace"},
		Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	left, err := canonicalIssueTableFingerprint("workspace-1", spec)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalIssueTableFingerprint("workspace-2", spec)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("equivalent queries in different workspaces produced the same fingerprint")
	}
}

func TestIssueTableCursorRejectsAnotherQuery(t *testing.T) {
	groupKey := "status:todo"
	cursor := issueTableCursor{
		Version:          1,
		QueryFingerprint: "sha256:old",
		GroupKey:         &groupKey,
	}
	w := httptest.NewRecorder()
	if issueTableCursorMatches(w, &cursor, "sha256:new", &groupKey, nil) {
		t.Fatal("cursor from another query unexpectedly matched")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestIssueTableRowsCommitsBeforeBestEffortEnrichment(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Table enrichment %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

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
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'table-enrichment', 'todo', 'none', 'member', $2, 1, $3, $4)
	`, testWorkspaceID, testUserID, issueNumber, projectID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	labelCalls := 0
	handler := *testHandler
	handler.TxStarter = issueTableEnrichmentFailTxStarter{
		inner:      testHandler.TxStarter,
		labelCalls: &labelCalls,
	}
	recorder := httptest.NewRecorder()
	handler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope: issueTableScope{Kind: "project", ProjectID: projectID},
			Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:     issueTableGroupSpec{Kind: "none"},
		Hierarchy: issueTableHierarchyRequest{Enabled: false},
		Page:      issueTablePageRequest{Limit: 10},
	}))

	if recorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if labelCalls != 0 {
		t.Fatalf("best-effort labels ran inside snapshot transaction %d times", labelCalls)
	}
	var response issueTableRowsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(response.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(response.Rows))
	}
}

func TestIssueTableStatusGroupingOverOneThousandRows(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Server table grouping %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 1001
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}
	firstNumber := finalNumber - 1000
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		SELECT $1, 'server-table-' || n::text,
		       CASE WHEN n <= 501 THEN 'todo' ELSE 'done' END,
		       'none', 'member', $2, n::double precision,
		       $3 + n - 1, $4
		FROM generate_series(1, 1001) AS n
	`, testWorkspaceID, testUserID, firstNumber, projectID); err != nil {
		t.Fatalf("seed issues: %v", err)
	}

	query := issueTableQuerySpec{
		Scope:   issueTableScope{Kind: "project", ProjectID: projectID},
		Filters: issueTableFiltersRequest{},
		Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
	}
	w := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(w, newRequest("POST", "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query,
		Group: issueTableGroupSpec{Kind: "status"},
		Page:  issueTablePageRequest{Limit: 100},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("groups status = %d: %s", w.Code, w.Body.String())
	}
	var groups issueTableGroupsResponse
	if err := json.NewDecoder(w.Body).Decode(&groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if groups.Total != 1001 {
		t.Fatalf("total = %d, want 1001", groups.Total)
	}
	counts := map[string]int64{}
	for _, group := range groups.Groups {
		counts[group.Key] = group.Count
	}
	if counts["status:todo"] != 501 || counts["status:done"] != 500 {
		t.Fatalf("unexpected group counts: %#v", counts)
	}
	firstGroupPageRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(firstGroupPageRecorder, newRequest("POST", "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query,
		Group: issueTableGroupSpec{Kind: "status"},
		Page:  issueTablePageRequest{Limit: 1},
	}))
	var firstGroupPage issueTableGroupsResponse
	if firstGroupPageRecorder.Code != http.StatusOK {
		t.Fatalf("first group page status = %d: %s", firstGroupPageRecorder.Code, firstGroupPageRecorder.Body.String())
	}
	if err := json.NewDecoder(firstGroupPageRecorder.Body).Decode(&firstGroupPage); err != nil {
		t.Fatalf("decode first group page: %v", err)
	}
	if len(firstGroupPage.Groups) != 1 || firstGroupPage.NextCursor == nil {
		t.Fatalf("unexpected first group page: %#v", firstGroupPage)
	}
	secondGroupPageRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(secondGroupPageRecorder, newRequest("POST", "/api/issues/table/groups", issueTableGroupsRequest{
		Query: query,
		Group: issueTableGroupSpec{Kind: "status"},
		Page:  issueTablePageRequest{Limit: 1, Cursor: firstGroupPage.NextCursor},
	}))
	var secondGroupPage issueTableGroupsResponse
	if secondGroupPageRecorder.Code != http.StatusOK {
		t.Fatalf("second group page status = %d: %s", secondGroupPageRecorder.Code, secondGroupPageRecorder.Body.String())
	}
	if err := json.NewDecoder(secondGroupPageRecorder.Body).Decode(&secondGroupPage); err != nil {
		t.Fatalf("decode second group page: %v", err)
	}
	if len(secondGroupPage.Groups) != 1 || secondGroupPage.Groups[0].Key == firstGroupPage.Groups[0].Key || secondGroupPage.Total != 1001 {
		t.Fatalf("group keyset pagination mismatch: first=%#v second=%#v", firstGroupPage, secondGroupPage)
	}

	groupKey := "status:todo"
	rowsRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(rowsRecorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query:     query,
		Group:     issueTableGroupSpec{Kind: "status"},
		GroupKey:  &groupKey,
		Hierarchy: issueTableHierarchyRequest{Enabled: false},
		Page:      issueTablePageRequest{Limit: 50},
	}))
	if rowsRecorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", rowsRecorder.Code, rowsRecorder.Body.String())
	}
	var rows issueTableRowsResponse
	if err := json.NewDecoder(rowsRecorder.Body).Decode(&rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if rows.Total != 1001 || rows.BranchTotal != 501 || len(rows.Rows) != 50 || rows.NextCursor == nil {
		t.Fatalf("unexpected rows page: total=%d branch_total=%d rows=%d cursor=%v", rows.Total, rows.BranchTotal, len(rows.Rows), rows.NextCursor)
	}
	firstPageIDs := make(map[string]struct{}, len(rows.Rows))
	for _, row := range rows.Rows {
		firstPageIDs[row.Issue.ID] = struct{}{}
	}
	secondRowsRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableRows(secondRowsRecorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query:     query,
		Group:     issueTableGroupSpec{Kind: "status"},
		GroupKey:  &groupKey,
		Hierarchy: issueTableHierarchyRequest{Enabled: false},
		Page:      issueTablePageRequest{Limit: 50, Cursor: rows.NextCursor},
	}))
	if secondRowsRecorder.Code != http.StatusOK {
		t.Fatalf("second rows status = %d: %s", secondRowsRecorder.Code, secondRowsRecorder.Body.String())
	}
	var secondRows issueTableRowsResponse
	if err := json.NewDecoder(secondRowsRecorder.Body).Decode(&secondRows); err != nil {
		t.Fatalf("decode second rows: %v", err)
	}
	if len(secondRows.Rows) != 50 {
		t.Fatalf("second rows length = %d, want 50", len(secondRows.Rows))
	}
	for _, row := range secondRows.Rows {
		if _, duplicate := firstPageIDs[row.Issue.ID]; duplicate {
			t.Fatalf("keyset cursor repeated issue %s across pages", row.Issue.ID)
		}
	}

	for _, sortCase := range []issueTableSortRequest{
		{Field: "status", Direction: "desc"},
		{Field: "created_at", Direction: "desc"},
		{Field: "due_date", Direction: "asc"},
	} {
		sortQuery := query
		sortQuery.Sort = sortCase
		fetchPage := func(cursor *string) issueTableRowsResponse {
			t.Helper()
			recorder := httptest.NewRecorder()
			testHandler.ListIssueTableRows(recorder, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
				Query:     sortQuery,
				Group:     issueTableGroupSpec{Kind: "status"},
				GroupKey:  &groupKey,
				Hierarchy: issueTableHierarchyRequest{Enabled: false},
				Page:      issueTablePageRequest{Limit: 10, Cursor: cursor},
			}))
			if recorder.Code != http.StatusOK {
				t.Fatalf("%s cursor page status = %d: %s", sortCase.Field, recorder.Code, recorder.Body.String())
			}
			var response issueTableRowsResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode %s cursor page: %v", sortCase.Field, err)
			}
			return response
		}
		first := fetchPage(nil)
		if len(first.Rows) != 10 || first.NextCursor == nil {
			t.Fatalf("%s first cursor page is incomplete", sortCase.Field)
		}
		seen := make(map[string]struct{}, len(first.Rows))
		for _, row := range first.Rows {
			seen[row.Issue.ID] = struct{}{}
		}
		second := fetchPage(first.NextCursor)
		if len(second.Rows) != 10 {
			t.Fatalf("%s second cursor page length = %d", sortCase.Field, len(second.Rows))
		}
		for _, row := range second.Rows {
			if _, duplicate := seen[row.Issue.ID]; duplicate {
				t.Fatalf("%s keyset cursor repeated issue %s", sortCase.Field, row.Issue.ID)
			}
		}
	}

	filteredQuery := query
	filteredQuery.Filters.Statuses = []string{"todo"}
	facetsRecorder := httptest.NewRecorder()
	testHandler.ListIssueTableFacets(facetsRecorder, newRequest("POST", "/api/issues/table/facets", issueTableFacetsRequest{
		Query:  filteredQuery,
		Facets: []issueTableFacetSpec{{Kind: "status"}},
	}))
	if facetsRecorder.Code != http.StatusOK {
		t.Fatalf("facets status = %d: %s", facetsRecorder.Code, facetsRecorder.Body.String())
	}
	var facets issueTableFacetsResponse
	if err := json.NewDecoder(facetsRecorder.Body).Decode(&facets); err != nil {
		t.Fatalf("decode facets: %v", err)
	}
	if facets.Total != 501 || len(facets.Facets) != 1 {
		t.Fatalf("unexpected filtered facet response: total=%d facets=%d", facets.Total, len(facets.Facets))
	}
	facetCounts := map[string]int64{}
	for _, value := range facets.Facets[0].Values {
		facetCounts[value.Key] = value.Count
	}
	if facetCounts["todo"] != 501 || facetCounts["done"] != 500 {
		t.Fatalf("status facet must ignore its own active filter: %#v", facetCounts)
	}
}

func TestIssueTableHierarchyDoesNotCrossGroups(t *testing.T) {
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Server table cross-group hierarchy')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 2
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}
	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'Todo parent', 'todo', 'none', 'member', $2, 1, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, finalNumber-1, projectID).Scan(&parentID); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	var childID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_issue_id, position, number, project_id
		)
		VALUES ($1, 'Done child', 'done', 'none', 'member', $2, $3, 2, $4, $5)
		RETURNING id
	`, testWorkspaceID, testUserID, parentID, finalNumber, projectID).Scan(&childID); err != nil {
		t.Fatalf("create child: %v", err)
	}

	query := issueTableQuerySpec{
		Scope:   issueTableScope{Kind: "project", ProjectID: projectID},
		Filters: issueTableFiltersRequest{},
		Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	listGroup := func(groupKey string) issueTableRowsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListIssueTableRows(w, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
			Query:     query,
			Group:     issueTableGroupSpec{Kind: "status"},
			GroupKey:  &groupKey,
			Hierarchy: issueTableHierarchyRequest{Enabled: true},
			Page:      issueTablePageRequest{Limit: 50},
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: status=%d body=%s", groupKey, w.Code, w.Body.String())
		}
		var response issueTableRowsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode %s: %v", groupKey, err)
		}
		return response
	}

	doneRows := listGroup("status:done")
	if len(doneRows.Rows) != 1 || doneRows.Rows[0].Issue.ID != childID {
		t.Fatalf("done child must become a root in its own group: %#v", doneRows.Rows)
	}
	todoRows := listGroup("status:todo")
	if len(todoRows.Rows) != 1 || todoRows.Rows[0].Issue.ID != parentID {
		t.Fatalf("todo group root mismatch: %#v", todoRows.Rows)
	}
	if todoRows.Rows[0].DirectChildCount != 0 {
		t.Fatalf("cross-group child leaked into todo parent count: %d", todoRows.Rows[0].DirectChildCount)
	}
}

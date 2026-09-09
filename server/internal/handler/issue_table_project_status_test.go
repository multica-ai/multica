package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// The project-status filter is a dimension of its own, next to the
// project-id filter: it keeps issues whose parent project currently sits in
// one of the selected `ProjectStatus` values. Combining it with any other
// filter is an AND, and an issue with no project can never satisfy it.
func TestIssueTableRowsFilterByProjectStatus(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	metadata := fmt.Sprintf(`{"project_status_filter_test":%q}`, fmt.Sprintf("pstatus-%d", suffix))

	createProject := func(title, status string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title, status) VALUES ($1, $2, $3) RETURNING id
		`, testWorkspaceID, title, status).Scan(&id); err != nil {
			t.Fatalf("create project %q: %v", title, err)
		}
		return id
	}
	activeProject := createProject(fmt.Sprintf("pstatus active %d", suffix), "in_progress")
	plannedProject := createProject(fmt.Sprintf("pstatus planned %d", suffix), "planned")
	doneProject := createProject(fmt.Sprintf("pstatus done %d", suffix), "completed")

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2, $3)`,
			activeProject, plannedProject, doneProject)
	})

	insertIssue := func(title string, projectID *string) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id, metadata)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5, $6::jsonb)
			RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID, metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	activeIssue := insertIssue("pstatus active issue", &activeProject)
	plannedIssue := insertIssue("pstatus planned issue", &plannedProject)
	doneIssue := insertIssue("pstatus done issue", &doneProject)
	orphanIssue := insertIssue("pstatus no project issue", nil)

	rows := func(filters issueTableFiltersRequest) []string {
		t.Helper()
		// Scope the window to this fixture so the shared workspace's other
		// issues cannot drift the assertion.
		filters.Properties = nil
		w := httptest.NewRecorder()
		testHandler.ListIssueTableRows(w, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: filters,
				Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 100},
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("rows status = %d: %s", w.Code, w.Body.String())
		}
		var response issueTableRowsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode rows: %v", err)
		}
		fixture := map[string]struct{}{
			activeIssue: {}, plannedIssue: {}, doneIssue: {}, orphanIssue: {},
		}
		ids := make([]string, 0, len(response.Rows))
		for _, row := range response.Rows {
			if _, ok := fixture[row.Issue.ID]; ok {
				ids = append(ids, row.Issue.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}

	assertRows := func(name string, filters issueTableFiltersRequest, want ...string) {
		t.Helper()
		got := rows(filters)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	assertRows("no filter", issueTableFiltersRequest{},
		activeIssue, plannedIssue, doneIssue, orphanIssue)
	// The orphan issue is absent: `project_id IS NULL` makes the EXISTS
	// predicate false, so "no project" is never an active project.
	assertRows("single status",
		issueTableFiltersRequest{ProjectStatuses: []string{"in_progress"}},
		activeIssue)
	assertRows("multiple statuses OR within the dimension",
		issueTableFiltersRequest{ProjectStatuses: []string{"in_progress", "planned"}},
		activeIssue, plannedIssue)
	// AND with the existing project-id filter, which stays a separate dimension.
	assertRows("combined with project ids",
		issueTableFiltersRequest{
			ProjectIDs:      []string{activeProject, plannedProject},
			ProjectStatuses: []string{"planned"},
		},
		plannedIssue)
	assertRows("combined filters with no overlap",
		issueTableFiltersRequest{
			ProjectIDs:      []string{doneProject},
			ProjectStatuses: []string{"in_progress"},
		})
	// `include_no_project` widens the project-id dimension only; the
	// project-status predicate still excludes the projectless issue.
	assertRows("include_no_project does not bypass the status predicate",
		issueTableFiltersRequest{
			ProjectIDs:       []string{activeProject},
			IncludeNoProject: true,
			ProjectStatuses:  []string{"in_progress"},
		},
		activeIssue)
}

func TestIssueTableRowsRejectsUnknownProjectStatus(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.ListIssueTableRows(w, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope: issueTableScope{Kind: "workspace"},
			// "backlog" is an issue status, not a project status — the project
			// lifecycle has no such value.
			Filters: issueTableFiltersRequest{ProjectStatuses: []string{"backlog"}},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 50},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

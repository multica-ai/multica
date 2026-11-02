package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The project-status filter is a dimension of its own, next to the
// project-id filter: it keeps issues whose parent project currently sits in
// one of the selected `ProjectStatus` values. Combining it with any other
// filter is an AND, and an issue with no project can never satisfy it.
func TestIssueTableRowsFilterByProjectStatus(t *testing.T) {
	suffix := time.Now().UnixNano()
	project := func(status string) string {
		return dbfx.Project(t, fmt.Sprintf("pstatus %s %d", status, suffix),
			testutil.Cols{"status": status})
	}
	activeProject := project("in_progress")
	plannedProject := project("planned")
	doneProject := project("completed")

	issue := func(title string, projectID any) string {
		return dbfx.Issue(t, fmt.Sprintf("%s %d", title, suffix),
			testutil.Cols{"project_id": projectID})
	}
	activeIssue := issue("pstatus active", activeProject)
	plannedIssue := issue("pstatus planned", plannedProject)
	doneIssue := issue("pstatus done", doneProject)
	orphanIssue := issue("pstatus no project", nil)

	fixture := map[string]struct{}{
		activeIssue: {}, plannedIssue: {}, doneIssue: {}, orphanIssue: {},
	}
	// The workspace is shared, so read back only the rows this test wrote.
	rows := func(filters issueTableFiltersRequest) []string {
		t.Helper()
		var response issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows,
			newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: issueTableQuerySpec{
					Scope:   issueTableScope{Kind: "workspace"},
					Filters: filters,
					Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
				},
				Group: issueTableGroupSpec{Kind: "none"},
				Page:  issueTablePageRequest{Limit: 100},
			}),
		).Want(http.StatusOK).JSON(&response)

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

// The schema carries no foreign keys, so `issue.project_id` can name a
// project in another workspace. That tenant's status must not decide this
// workspace's query membership.
func TestIssueTableRowsProjectStatusStaysInsideTheWorkspace(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherWorkspace := dbfx.Workspace(t,
		fmt.Sprintf("pstatus other %d", suffix),
		fmt.Sprintf("pstatus-other-%d", suffix))
	otherFixture := testutil.New(testPool, otherWorkspace, testUserID)
	foreignProject := otherFixture.Project(t, "pstatus foreign",
		testutil.Cols{"status": "in_progress"})

	strayIssue := dbfx.Issue(t, fmt.Sprintf("pstatus stray %d", suffix),
		testutil.Cols{"project_id": foreignProject})

	var response issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows,
		newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: issueTableFiltersRequest{ProjectStatuses: []string{"in_progress"}},
				Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 100},
		}),
	).Want(http.StatusOK).JSON(&response)

	for _, row := range response.Rows {
		if row.Issue.ID == strayIssue {
			t.Fatalf("issue pointing at another workspace's in_progress project matched the filter")
		}
	}
}

func TestIssueTableRowsRejectsUnknownProjectStatus(t *testing.T) {
	testutil.Call(t, testHandler.ListIssueTableRows,
		newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope: issueTableScope{Kind: "workspace"},
				// "backlog" is an issue status, not a project status — the
				// project lifecycle has no such value.
				Filters: issueTableFiltersRequest{ProjectStatuses: []string{"backlog"}},
				Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 50},
		}),
	).Want(http.StatusBadRequest)
}
